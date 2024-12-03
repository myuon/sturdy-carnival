package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"
)

func RecordMicStream() {
	recordSeconds := 5
	sampleRate := 44100
	numChannels := 1
	framesPerBuffer := 64

	// WAVファイルの作成
	file, err := os.Create("output.wav")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// WAVファイルのヘッダーを書き込む
	writeWavHeader(file, numChannels, sampleRate, 16, uint32(recordSeconds*sampleRate*numChannels*2))

	// PortAudioの初期化
	portaudio.Initialize()
	defer portaudio.Terminate()

	// 入力ストリームの作成
	in := make([]int16, framesPerBuffer*numChannels)
	stream, err := portaudio.OpenDefaultStream(numChannels, 0, float64(sampleRate), framesPerBuffer, func(inBuf, outBuf []int16) {
		copy(in, inBuf)
		// WAVファイルにデータを書き込む
		binary.Write(file, binary.LittleEndian, in)
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
}

// WAVファイルのヘッダーを書き込む関数
func writeWavHeader(file *os.File, numChannels, sampleRate, bitsPerSample int, dataSize uint32) {
	// RIFFチャンク
	file.WriteString("RIFF")
	binary.Write(file, binary.LittleEndian, uint32(36+dataSize))
	file.WriteString("WAVE")

	// fmtチャンク
	file.WriteString("fmt ")
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
	file.WriteString("data")
	binary.Write(file, binary.LittleEndian, dataSize)
}
