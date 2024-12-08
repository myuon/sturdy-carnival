package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
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
	noSpeechDuration := 1500 * time.Millisecond
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

type App struct {
	aiClient     *genai.Client
	geminiModel  *genai.GenerativeModel
	ttsClient    *texttospeech.Client
	speechClient *speech.Client
}

func (app *App) Init() error {
	if err := portaudio.Initialize(); err != nil {
		return err
	}

	projectId := "default-364617"
	region := "asia-northeast1"
	modelName := "gemini-1.5-pro"
	client, err := genai.NewClient(context.Background(), projectId, region)
	if err != nil {
		return fmt.Errorf("error creating client: %w", err)
	}
	gemini := client.GenerativeModel(modelName)
	gemini.SetTemperature(0)
	gemini.SystemInstruction = genai.NewUserContent(genai.Text(`You are a customer staff to support guests who are traveling in Japan.
Keep the following in mind when responding to guests:
- Response should be short and concise.
- Response shoudl be in the same language as the input.
- If you don't know the answer, please ask for the staff (use AskForStaff tool) for help.
`))

	lockTool := &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "GetDoorLockNumber",
			Description: "Get the number of the door lock by the reservation code. If the guest forgets the number, please use this tool.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"reservationCode": {
						Type:        genai.TypeString,
						Description: "The reservation code of the guest.",
					},
				},
				Required: []string{"reservationCode"},
			},
		},
			{
				Name:        "AskForStaff",
				Description: "Ask for the staff for help. If you don't know the answer, please use this tool.",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"reservationCode": {
							Type:        genai.TypeString,
							Description: "The reservation code of the guest.",
						},
						"message": {
							Type:        genai.TypeString,
							Description: "The message to ask for the staff.",
						},
						"callType": {
							Type:        genai.TypeString,
							Description: "The type of the call.",
							Enum:        []string{"trouble_in_stay", "not_enough_information", "emergency"},
						},
					},
					Required: []string{"message", "callType"},
				},
			},
		},
	}

	gemini.Tools = []*genai.Tool{lockTool}

	app.geminiModel = gemini
	app.aiClient = client

	ttsClient, err := texttospeech.NewClient(context.Background())
	if err != nil {
		return fmt.Errorf("error creating text-to-speech client: %w", err)
	}

	app.ttsClient = ttsClient

	speechClient, err := speech.NewClient(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	app.speechClient = speechClient

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

func (app *App) RunTextToSpeech(langCode string, text string) error {
	req := &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{Text: text},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: langCode,
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

func (app *App) RunSpeechToText(f io.Reader) (string, string, error) {
	stream, err := app.speechClient.StreamingRecognize(context.Background())
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
					AlternativeLanguageCodes: []string{
						"ja-JP",
					},
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
	langCode := ""

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
			langCode = result.LanguageCode
			transcript = result.Alternatives[0].Transcript
		}
	}

	return langCode, transcript, nil
}

func (app *App) StartChat() *genai.ChatSession {
	return app.geminiModel.StartChat()
}

func main() {
	app := App{}

	if err := app.Init(); err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	chat := app.StartChat()

	for {
		reader, writer := io.Pipe()

		go func(writer io.WriteCloser) {
			if err := RecordMicStream(writer); err != nil {
				log.Fatal(err)
			}

			writer.Close()
		}(writer)

		langCode, transcript, err := app.RunSpeechToText(reader)
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("You: %v", transcript)
		if len(transcript) == 0 {
			continue
		}

		resp, err := chat.SendMessage(context.Background(), genai.Text(transcript))
		if err != nil {
			log.Fatal(err)
		}

		respBs, _ := json.Marshal(&resp)
		log.Printf("[DEBUG] Response: %v", string(respBs))

		part := resp.Candidates[0].Content.Parts[0]
		switch part := part.(type) {
		case genai.Text:
			content := string(part)
			log.Printf("AI: %s", content)

			if err := app.RunTextToSpeech(langCode, string(content)); err != nil {
				log.Fatal(err)
			}
		case genai.FunctionCall:
			switch part.Name {
			case "GetDoorLockNumber":
				reservationCode := part.Args["reservationCode"].(string)
				lockNumber := "4819"

				log.Printf("[FunctionCall] GetDoorLockNumber: %v -> %v", reservationCode, lockNumber)

				resp, err := chat.SendMessage(context.Background(), genai.FunctionResponse{
					Name: "GetDoorLockNumber",
					Response: map[string]any{
						"lockNumber": lockNumber,
					},
				})
				if err != nil {
					log.Fatal(err)
				}

				content := resp.Candidates[0].Content.Parts[0].(genai.Text)

				log.Printf("AI: %v", content)
				if err := app.RunTextToSpeech(langCode, string(content)); err != nil {
					log.Fatal(err)
				}
			case "AskForStaff":
				reservationCode := part.Args["reservationCode"].(string)
				message := part.Args["message"].(string)
				callType := part.Args["callType"].(string)

				log.Printf("[FunctionCall] AskForStaff: %v, %v, %v", reservationCode, message, callType)

				log.Print("YOU>")
				response := ""
				if _, err := fmt.Scanln(&response); err != nil {
					log.Fatal(err)
				}

				resp, err := chat.SendMessage(context.Background(), genai.FunctionResponse{
					Name: "AskForStaff",
					Response: map[string]any{
						"response": response,
					},
				})
				if err != nil {
					log.Fatal(err)
				}

				content := resp.Candidates[0].Content.Parts[0].(genai.Text)

				log.Printf("AI: %v", content)
				if err := app.RunTextToSpeech(langCode, string(content)); err != nil {
					log.Fatal(err)
				}
			}
		default:
			log.Printf("Unknown part: %v", part)
		}
	}
}
