package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/gordonklaus/portaudio"
)

func RecordMicStream(writer io.Writer) error {
	recordSeconds := 5
	sampleRate := 44100
	numChannels := 1
	framesPerBuffer := 64

	// WAVファイルのヘッダーを書き込む
	writeWavHeader(writer, numChannels, sampleRate, 16, uint32(recordSeconds*sampleRate*numChannels*2))

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

// WAVファイルのヘッダーを書き込む関数
func writeWavHeader(file io.Writer, numChannels, sampleRate, bitsPerSample int, dataSize uint32) {
	// RIFFチャンク
	io.WriteString(file, "RIFF")
	binary.Write(file, binary.LittleEndian, uint32(36+dataSize))
	io.WriteString(file, "WAVE")

	// fmtチャンク
	io.WriteString(file, "fmt ")
	binary.Write(file, binary.LittleEndian, uint32(16)) // fmtチャンクのサイズ
	binary.Write(file, binary.LittleEndian, uint16(1))  // フォーマットID（リニアPCM）
	binary.Write(file, binary.LittleEndian, uint16(numChannels))
	binary.Write(file, binary.LittleEndian, uint32(sampleRate))
	byteRate := uint32(sampleRate * numChannels * bitsPerSample / 8)
	binary.Write(file, binary.LittleEndian, byteRate)
	blockAlign := uint16(numChannels * bitsPerSample / 8)
	binary.Write(file, binary.LittleEndian, blockAlign)
	binary.Write(file, binary.LittleEndian, uint16(bitsPerSample))

	// dataチャンク
	io.WriteString(file, "data")
	binary.Write(file, binary.LittleEndian, dataSize)
}
