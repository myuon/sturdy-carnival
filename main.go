package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/myuon/voicebot-ai-cli/voicebot"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"cloud.google.com/go/vertexai/genai"
	"github.com/gordonklaus/portaudio"
)

var (
	sampleRate = 44100
)

func RecordMicStream(writer io.Writer) error {
	numChannels := 1
	framesPerBuffer := 64
	noSpeechDuration := 3500 * time.Millisecond
	lastSpeechTime := time.Now()

	// 入力ストリームの作成
	in := make([]int16, framesPerBuffer*numChannels)
	stream, err := portaudio.OpenDefaultStream(numChannels, 0, float64(sampleRate), framesPerBuffer, func(inBuf, outBuf []int16) {
		copy(in, inBuf)
		// WAVファイルにデータを書き込む
		binary.Write(writer, binary.LittleEndian, in)

		// Check if there is sound input
		if isSpeech(in) {
			lastSpeechTime = time.Now()
		}
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	// 録音開始
	err = stream.Start()
	if err != nil {
		panic(err)
	}
	fmt.Println("録音中...")

	for {
		if time.Since(lastSpeechTime) > noSpeechDuration {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	err = stream.Stop()
	if err != nil {
		panic(err)
	}
	fmt.Println("録音完了")

	return nil
}

func isSpeech(samples []int16) bool {
	threshold := int16(1000)
	for _, sample := range samples {
		if sample > threshold || sample < -threshold {
			return true
		}
	}
	return false
}

func RunSpeechToText(f io.Reader) (string, error) {
	ctx := context.Background()

	client, err := speech.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	stream, err := client.StreamingRecognize(ctx)
	if err != nil {
		log.Fatal(err)
	}
	// Send the initial configuration message.
	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        speechpb.RecognitionConfig_LINEAR16,
					SampleRateHertz: int32(sampleRate),
					LanguageCode:    "en-US",
				},
				InterimResults: true,
			},
		},
	}); err != nil {
		log.Fatal(err)
	}

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if err := stream.Send(&speechpb.StreamingRecognizeRequest{
					StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
						AudioContent: buf[:n],
					},
				}); err != nil {
					log.Printf("Could not send audio: %v", err)
				}
			}
			if err == io.EOF {
				// Nothing else to pipe, close the stream.
				if err := stream.CloseSend(); err != nil {
					log.Fatalf("Could not close stream: %v", err)
				}
				return
			}
			if err != nil {
				log.Printf("Could not read from %v", err)
				continue
			}
		}
	}()

	transcript := ""

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			log.Printf("End of stream")
			break
		}
		if err != nil {
			log.Fatalf("Cannot stream results: %v", err)
		}
		if err := resp.Error; err != nil {
			log.Fatalf("Could not recognize: %v", err)
		}
		for _, result := range resp.Results {
			fmt.Printf("Result: %+v\n", result)
			transcript = result.Alternatives[0].Transcript
		}
	}

	return transcript, nil
}

type App struct {
	aiClient    *genai.Client
	geminiModel *genai.GenerativeModel
	ttsClient   *texttospeech.Client
}

func (app *App) Init() error {
	if err := portaudio.Initialize(); err != nil {
		return err
	}

	projectId := "default-364617"
	region := "asia-northeast1"
	modelName := "gemini-1.0-pro"
	client, err := genai.NewClient(context.Background(), projectId, region)
	if err != nil {
		return fmt.Errorf("error creating client: %w", err)
	}
	gemini := client.GenerativeModel(modelName)

	app.geminiModel = gemini
	app.aiClient = client

	ttsClient, err := texttospeech.NewClient(context.Background())
	if err != nil {
		return fmt.Errorf("error creating text-to-speech client: %w", err)
	}

	app.ttsClient = ttsClient

	return nil
}

func (app *App) Close() error {
	if err := portaudio.Terminate(); err != nil {
		return err
	}

	app.aiClient.Close()
	app.ttsClient.Close()

	return nil
}

func (app *App) GetGeminiResponse(query string) (string, error) {
	prompt := genai.Text(query)
	resp, err := app.geminiModel.GenerateContent(context.Background(), prompt)
	if err != nil {
		return "", fmt.Errorf("error generating content: %w", err)
	}

	text := resp.Candidates[0].Content.Parts[0].(genai.Text)

	return string(text), nil
}

func (app *App) RunTextToSpeech(text string) error {
	req := &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{Text: text},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: "en-US",
			SsmlGender:   texttospeechpb.SsmlVoiceGender_NEUTRAL,
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding: texttospeechpb.AudioEncoding_LINEAR16,
		},
	}

	resp, err := app.ttsClient.SynthesizeSpeech(context.Background(), req)
	if err != nil {
		return fmt.Errorf("error synthesizing speech: %w", err)
	}

	contentBuffer := bytes.NewBuffer(resp.AudioContent)

	header, err := voicebot.ReadWavHeader(contentBuffer)
	if err != nil {
		return err
	}

	// オーディオデータを読み取る
	audioData := make([]byte, header.DataSize)
	if _, err := io.ReadFull(contentBuffer, audioData); err != nil {
		log.Fatalf("オーディオデータの読み取りに失敗しました: %v", err)
	}

	int16Data := make([]int16, len(resp.AudioContent)/2)
	if err := binary.Read(bytes.NewReader(resp.AudioContent), binary.LittleEndian, &int16Data); err != nil {
		log.Fatalf("バイトデータの読み取りに失敗しました: %v", err)
	}

	outDevice, err := portaudio.DefaultOutputDevice()
	if err != nil {
		log.Fatalf("デフォルトの出力デバイスの取得に失敗しました: %v", err)
	}

	// ストリームのパラメータを設定
	out := portaudio.StreamDeviceParameters{
		Device:   outDevice,
		Channels: int(header.Channels),
		Latency:  outDevice.DefaultLowOutputLatency,
	}

	// ストリームを開く
	stream, err := portaudio.OpenStream(portaudio.StreamParameters{
		Output:          out,
		SampleRate:      float64(header.SampleRate),
		FramesPerBuffer: len(int16Data),
	}, func(out []int16) {
		copy(out, int16Data)
	})
	if err != nil {
		log.Fatalf("ストリームのオープンに失敗しました: %v", err)
	}
	defer stream.Close()

	// ストリームを開始
	err = stream.Start()
	if err != nil {
		log.Fatalf("ストリームの開始に失敗しました: %v", err)
	}

	time.Sleep(time.Duration(header.DurationSeconds()) * time.Second)

	// ストリームを停止
	err = stream.Stop()
	if err != nil {
		log.Fatalf("ストリームの停止に失敗しました: %v", err)
	}

	return nil
}

func main() {
	app := App{}

	if err := app.Init(); err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	bs := []byte{}
	buffer := bytes.NewBuffer(bs)

	for {
		if err := RecordMicStream(buffer); err != nil {
			log.Fatal(err)
		}

		transcript, err := RunSpeechToText(buffer)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("You: %s", transcript)

		resp, err := app.GetGeminiResponse(transcript)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("AI: %v", resp)

		if err := app.RunTextToSpeech(resp); err != nil {
			log.Fatal(err)
		}

		time.Sleep(1 * time.Second)
	}
}
