package main

import (
	"fmt"
	"io"
	"os"

	mp3codec "github.com/godexture/codec-mp3"
	pcm "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	mp3format "github.com/godexture/format-mp3"
	wav "github.com/godexture/format-wav"
)

func main() {
	inputPath := "example/assets/mpeg3.mp3"
	outputPath := "example/out_mp3.wav"

	if len(os.Args) >= 2 {
		inputPath = os.Args[1]
	}
	if len(os.Args) >= 3 {
		outputPath = os.Args[2]
	}

	fmt.Printf("Converting %s to %s\n", inputPath, outputPath)

	f, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("Failed to open input: %v\n", err)
		return
	}
	defer f.Close()

	demux, err := mp3format.NewDemuxerEngine(f)
	if err != nil {
		fmt.Printf("Failed to create mp3 demuxer: %v\n", err)
		return
	}

	streams, _, err := demux.Analyze()
	if err != nil {
		fmt.Printf("Failed to analyze input: %v\n", err)
		return
	}

	if len(streams) == 0 {
		fmt.Println("No audio streams found")
		return
	}

	// MP3 デコーダ
	dec := mp3codec.NewDecoderEngine(mp3codec.DecoderConfig{})

	// PCM エンコーダ (LPCM)
	enc := pcm.NewEncoderEngine(pcm.EncoderConfig{CodecID: media.CodecLPCM})

	outF, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outF.Close()

	// WAV Muxer
	mux := wav.NewMuxerEngine(outF)
	outStream := streams[0]
	// デコード後およびLPCMエンコード後の属性を設定
	outStream.Codec = media.CodecLPCM
	outStream.Audio.CodecID = media.CodecLPCM
	// go-mp3は S16 LE を出力するため、エンコーダもそのままスルーする
	outStream.Audio.Format = media.SampleFormatS16

	if _, err := mux.AddStream(outStream); err != nil {
		fmt.Printf("Failed to add stream to muxer: %v\n", err)
		return
	}
	if err := mux.WriteHeader(); err != nil {
		fmt.Printf("Failed to write wav header: %v\n", err)
		return
	}

	packetCount := 0
	for {
		pkt, _, err := demux.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("Error reading packet: %v\n", err)
			break
		}

		if err := dec.SendPacket(pkt); err != nil {
			fmt.Printf("Error sending packet to decoder: %v\n", err)
			break
		}

		frame, err := dec.ReceiveFrame()
		if err != nil {
			fmt.Printf("Error receiving frame from decoder: %v\n", err)
			break
		}

		if err := enc.SendFrame(frame); err != nil {
			fmt.Printf("Error sending frame to encoder: %v\n", err)
			break
		}

		outPkt, err := enc.ReceivePacket()
		if err != nil {
			fmt.Printf("Error receiving packet from encoder: %v\n", err)
			break
		}

		if err := mux.WritePacket(0, outPkt); err != nil {
			fmt.Printf("Error writing packet to muxer: %v\n", err)
			break
		}
		packetCount++
	}

	if err := mux.WriteTrailer(); err != nil {
		fmt.Printf("Failed to write wav trailer: %v\n", err)
		return
	}

	fmt.Printf("Successfully converted %s to %s (processed %d packets)\n", inputPath, outputPath, packetCount)
}
