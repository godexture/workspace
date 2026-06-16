package main

import (
	"fmt"
	"io"
	"os"

	mp3Codec "github.com/godexture/codec-mp3"
	pcm "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	mp3Format "github.com/godexture/format-mp3"
	wavFormat "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
)

func main() {
	inputPath := "assets/mpeg3.mp3"
	outputPath := "out_mp3.wav"

	if len(os.Args) >= 2 {
		inputPath = os.Args[1]
	}
	if len(os.Args) >= 3 {
		outputPath = os.Args[2]
	}

	fmt.Printf("Converting %s to %s\n", inputPath, outputPath)

	file, err := os.Open(inputPath)
	if err != nil {
		fmt.Printf("Failed to open input: %v\n", err)
		return
	}
	defer file.Close()

	demuxer, err := mp3Format.NewDemuxerEngine(file)
	if err != nil {
		fmt.Printf("Failed to create mp3 demuxer: %v\n", err)
		return
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		fmt.Printf("Failed to analyze input: %v\n", err)
		return
	}

	if len(streams) == 0 {
		fmt.Println("No audio streams found")
		return
	}

	// MP3 デコーダ
	decoder := mp3Codec.NewDecoderEngine(mp3Codec.DecoderConfig{})

	// PCM エンコーダ (LPCM)
	encoder := pcm.NewEncoderEngine(pcm.EncoderConfig{CodecID: media.CodecLPCM})

	outputFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Failed to create output: %v\n", err)
		return
	}
	defer outputFile.Close()

	// WAV Muxer
	muxer := wavFormat.NewMuxerEngine(outputFile)
	outputStream := streams[0]
	// デコード後およびLPCMエンコード後の属性を設定
	outputStream.Codec = media.CodecLPCM
	outputStream.Audio.CodecID = media.CodecLPCM
	// デコーダは float32 PCM を出力するため、エンコーダもそのままスルーする
	outputStream.Audio.Format = media.SampleFormatF32

	if _, err := muxer.AddStream(outputStream); err != nil {
		fmt.Printf("Failed to add stream to muxer: %v\n", err)
		return
	}
	if err := muxer.WriteHeader(); err != nil {
		fmt.Printf("Failed to write wav header: %v\n", err)
		return
	}

	packetCount := 0
	for {
		packet, _, err := demuxer.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("Error reading packet: %v\n", err)
			break
		}

		if err := decoder.SendPacket(packet); err != nil {
			if err != engine.ErrEAGAIN {
				fmt.Printf("Error sending packet to decoder: %v\n", err)
				break
			}
		}

		for {
			frame, err := decoder.ReceiveFrame()
			if err == engine.ErrEAGAIN {
				break
			}
			if err != nil {
				fmt.Printf("Error receiving frame from decoder: %v\n", err)
				return
			}

			if err := encoder.SendFrame(frame); err != nil {
				fmt.Printf("Error sending frame to encoder: %v\n", err)
				return
			}

			for {
				outputPacket, err := encoder.ReceivePacket()
				if err == engine.ErrEAGAIN {
					break
				}
				if err != nil {
					fmt.Printf("Error receiving packet from encoder: %v\n", err)
					return
				}

				if err := muxer.WritePacket(0, outputPacket); err != nil {
					fmt.Printf("Error writing packet to muxer: %v\n", err)
					return
				}
				packetCount++
			}
		}
	}

	decoder.Flush()
	for {
		frame, err := decoder.ReceiveFrame()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			fmt.Printf("Error receiving frame from decoder during flush: %v\n", err)
			break
		}
		if err := encoder.SendFrame(frame); err == nil {
			for {
				outputPacket, err := encoder.ReceivePacket()
				if err == engine.ErrEAGAIN {
					break
				}
				if err != nil {
					break
				}
				muxer.WritePacket(0, outputPacket)
				packetCount++
			}
		}
	}

	encoder.Flush()
	for {
		outputPacket, err := encoder.ReceivePacket()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			break
		}
		muxer.WritePacket(0, outputPacket)
		packetCount++
	}

	if err := muxer.WriteTrailer(); err != nil {
		fmt.Printf("Failed to write wav trailer: %v\n", err)
		return
	}

	fmt.Printf("Successfully converted %s to %s (processed %d packets)\n", inputPath, outputPath, packetCount)
}
