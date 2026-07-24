package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestDescribeFilterReturnsParameterizedTopology(t *testing.T) {
	text, err := DescribeFilter("mixer", map[string]string{"in": "2", "out": "1"})
	if err != nil {
		t.Fatalf("DescribeFilter() error = %v", err)
	}
	var entry struct {
		Inputs  []string `json:"inputs"`
		Outputs []string `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(text), &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.Inputs) != 2 || len(entry.Outputs) != 1 {
		t.Fatalf("mixer topology = %#v", entry)
	}
}

func TestResolveAcceptsAuxiliaryInputs(t *testing.T) {
	spec := `{"muxer":{"name":"wav"},"auxInputs":{"ir":{}},"filters":[{"name":"convolver","inputs":{"ir":{"alias":"ir"}}}]}`
	resolved, err := Resolve(testWAV(), map[string][]byte{"ir": testWAV()}, spec)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	var description struct {
		Edges []struct {
			ToPort string
		}
	}
	if err := json.Unmarshal([]byte(resolved), &description); err != nil {
		t.Fatal(err)
	}
	for _, edge := range description.Edges {
		if edge.ToPort == "ir" {
			return
		}
	}
	t.Fatalf("resolved graph has no auxiliary input edge: %s", resolved)
}

func testWAV() []byte {
	const samples = 16
	var data bytes.Buffer
	data.WriteString("RIFF")
	_ = binary.Write(&data, binary.LittleEndian, uint32(36+samples*2))
	data.WriteString("WAVEfmt ")
	_ = binary.Write(&data, binary.LittleEndian, uint32(16))
	_ = binary.Write(&data, binary.LittleEndian, uint16(1))
	_ = binary.Write(&data, binary.LittleEndian, uint16(1))
	_ = binary.Write(&data, binary.LittleEndian, uint32(8000))
	_ = binary.Write(&data, binary.LittleEndian, uint32(16000))
	_ = binary.Write(&data, binary.LittleEndian, uint16(2))
	_ = binary.Write(&data, binary.LittleEndian, uint16(16))
	data.WriteString("data")
	_ = binary.Write(&data, binary.LittleEndian, uint32(samples*2))
	for i := 0; i < samples; i++ {
		_ = binary.Write(&data, binary.LittleEndian, int16(i*100))
	}
	return data.Bytes()
}
