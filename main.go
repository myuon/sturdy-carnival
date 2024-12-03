package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/gordonklaus/portaudio"
	"github.com/zenwerk/go-wave"
)

type BufioCloser struct {
	*bytes.Buffer
}

func (b BufioCloser) Close() error {
	return nil
}

func ReadMic(writer io.WriteCloser) error {
	// // form chunk
	// _, err := f.WriteString("FORM")
	// chk(err)
	// chk(binary.Write(f, binary.BigEndian, int32(0))) //total bytes
	// _, err = f.WriteString("AIFF")
	// chk(err)

	// // common chunk
	// _, err = f.WriteString("COMM")
	// chk(err)
	// chk(binary.Write(f, binary.BigEndian, int32(18)))                  //size
	// chk(binary.Write(f, binary.BigEndian, int16(1)))                   //channels
	// chk(binary.Write(f, binary.BigEndian, int32(0)))                   //number of samples
	// chk(binary.Write(f, binary.BigEndian, int16(32)))                  //bits per sample
	// _, err = f.Write([]byte{0x40, 0x0e, 0xac, 0x44, 0, 0, 0, 0, 0, 0}) //80-bit sample rate 44100
	// chk(err)

	// // sound chunk
	// _, err = f.WriteString("SSND")
	// chk(err)
	// chk(binary.Write(f, binary.BigEndian, int32(0))) //size
	// chk(binary.Write(f, binary.BigEndian, int32(0))) //offset
	// chk(binary.Write(f, binary.BigEndian, int32(0))) //block
	// nSamples := 0
	// defer func() {
	// 	// fill in missing sizes
	// 	totalBytes := 4 + 8 + 18 + 8 + 8 + 4*nSamples
	// 	_, err = f.Seek(4, 0)
	// 	chk(err)
	// 	chk(binary.Write(f, binary.BigEndian, int32(totalBytes)))
	// 	_, err = f.Seek(22, 0)
	// 	chk(err)
	// 	chk(binary.Write(f, binary.BigEndian, int32(nSamples)))
	// 	_, err = f.Seek(42, 0)
	// 	chk(err)
	// 	chk(binary.Write(f, binary.BigEndian, int32(4*nSamples+8)))
	// 	// chk(f.Close())
	// }()
	waveWriter, err := wave.NewWriter(wave.WriterParam{
		Out:           writer,
		Channel:       1,
		SampleRate:    44100,
		BitsPerSample: 8,
	})
	if err != nil {
		return err
	}

	frame := make([]byte, 64)
	stream, err := portaudio.OpenDefaultStream(1, 0, 44100, len(frame), frame)
	chk(err)
	defer stream.Close()

	chk(stream.Start())

	for {
		chk(stream.Read())
		if _, err := waveWriter.Write(frame); err != nil {
			return err
		}

		log.Printf("Writing %x bytes", frame)
	}
	chk(stream.Stop())

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
					SampleRateHertz: 16000,
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
	buffer := BufioCloser{Buffer: bytes.NewBuffer(bs)}

	go func() {
		if err := ReadMic(buffer); err != nil {
			log.Fatal(err)
		}
	}()

	if err := RunSpeechToText(buffer); err != nil {
		log.Fatal(err)
	}
}

func chk(err error) {
	if err != nil {
		panic(err)
	}
}
