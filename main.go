package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/gordonklaus/portaudio"
)

func RecordMicStream(writer io.Writer) error {
	recordSeconds := 5
	sampleRate := 44100
	numChannels := 1
	framesPerBuffer := 64

	// 入力ストリームの作成
	in := make([]int16, framesPerBuffer*numChannels)
	stream, err := portaudio.OpenDefaultStream(numChannels, 0, float64(sampleRate), framesPerBuffer, func(inBuf, outBuf []int16) {
		copy(in, inBuf)
		// WAVファイルにデータを書き込む
		binary.Write(writer, binary.LittleEndian, in)
	})
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	// 録音開始
	err = stream.Start()
	if err != nil {
		panic(err)
	}
	fmt.Println("録音中...")
	time.Sleep(time.Duration(recordSeconds) * time.Second)
	err = stream.Stop()
	if err != nil {
		panic(err)
	}
	fmt.Println("録音完了")

	return nil
}

func RunSpeechToText(f io.Reader) error {
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
					SampleRateHertz: 44100,
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
		}
	}
	return nil
}

func main() {
	portaudio.Initialize()
	defer portaudio.Terminate()

	bs := []byte{}
	buffer := bytes.NewBuffer(bs)

	if err := RecordMicStream(buffer); err != nil {
		log.Fatal(err)
	}

	if err := RunSpeechToText(buffer); err != nil {
		log.Fatal(err)
	}
}
