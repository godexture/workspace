package main

import (
	"fmt"
	"io"
	"os"

	pcm "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	wav "github.com/godexture/format-wav"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run example/pcm.go <input.wav> <output.wav> [pcma|pcmu]")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]
	codecType := "pcmu"
	if len(os.Args) > 3 {
		codecType = os.Args[3]
	}

	targetCodec := media.CodecPCMU
	if codecType == "pcma" {
		targetCodec = media.CodecPCMA
	}

	f, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("Failed to open input: %v\n", err)
		return
	}
	defer f.Close()

	demux, err := wav.NewDemuxerEngine(f)
	if err != nil {
		fmt.Printf("Failed to create demuxer: %v\n", err)
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

	a := streams[0].MediaAttributes.Audio
	dec := pcm.NewDecoderEngine(pcm.NewConfigWithAudio(a.SampleRate, a.Format, a.ChannelLayout))

	enc := pcm.NewEncoderEngine(pcm.EncoderConfig{CodecID: targetCodec})

	outF, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outF.Close()

	mux := wav.NewMuxerEngine(outF)
	outStream := streams[0]
	outStream.Codec = targetCodec
	outStream.Audio.CodecID = targetCodec
	outStream.Audio.Format = media.SampleFormatU8

	if _, err := mux.AddStream(outStream); err != nil {
		fmt.Printf("Failed to add stream to muxer: %v\n", err)
		return
	}
	if err := mux.WriteHeader(); err != nil {
		fmt.Printf("Failed to write wav header: %v\n", err)
		return
	}

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
	}

	if err := mux.WriteTrailer(); err != nil {
		fmt.Printf("Failed to write wav trailer: %v\n", err)
		return
	}

	fmt.Printf("Successfully converted %s to %s (%s)\n", inputPath, outputPath, codecType)
}
