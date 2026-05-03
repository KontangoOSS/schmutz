package main

import (
	"encoding/binary"
	"log"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/openziti/sdk-golang/ziti"
)

func main() {
	cfg, err := ziti.NewConfigFromFile("/tmp/schmutz-agent-test.json")
	if err != nil {
		log.Fatal("config:", err)
	}
	ctx, err := ziti.NewContext(cfg)
	if err != nil {
		log.Fatal("context:", err)
	}
	defer ctx.Close()

	conn, err := ctx.Dial("telemetry.tango")
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer conn.Close()
	log.Println("connected to telemetry.tango!")

	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))

	sendFrame := func(msgType uint8, payload []byte) {
		compressed := enc.EncodeAll(payload, nil)
		header := make([]byte, 13)
		header[0] = msgType
		binary.BigEndian.PutUint32(header[1:5], uint32(len(compressed)))
		binary.BigEndian.PutUint64(header[5:13], uint64(time.Now().UnixNano()))
		conn.Write(header)
		conn.Write(compressed)
	}

	// Send heartbeat
	sendFrame(0x01, nil)
	log.Println("sent heartbeat")

	// Send system frame
	system := `{"hostname":"test-machine","cpu_percent":42.5,"mem_total":17179869184,"mem_used":8589934592,"mem_percent":50.0,"load_avg_1":1.5,"uptime":86400,"num_cpu":4}`
	sendFrame(0x02, []byte(system))
	log.Println("sent system frame")

	// Send network frame
	network := `{"interfaces":[{"name":"eth0","bytes_sent":1000000,"bytes_recv":5000000,"ip":"10.0.0.1"}]}`
	sendFrame(0x03, []byte(network))
	log.Println("sent network frame")

	time.Sleep(5 * time.Second)
	log.Println("done")
}
