package clamav

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClamd поднимает TCP-сервер, понимающий INSTREAM ровно настолько, чтобы проверить наш клиент:
// читает чанки до нулевого, отвечает заданной строкой.
func fakeClamd(t *testing.T, reply string) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		r := bufio.NewReader(conn)
		cmd, _ := r.ReadString('\x00')
		if cmd != "zINSTREAM\x00" && cmd != "zPING\x00" {
			return
		}
		if cmd == "zPING\x00" {
			_, _ = conn.Write([]byte("PONG\x00"))
			return
		}

		var received bytes.Buffer
		lenBuf := make([]byte, 4)
		for {
			if _, err := io.ReadFull(r, lenBuf); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(lenBuf)
			if size == 0 {
				break
			}
			if _, err := io.CopyN(&received, r, int64(size)); err != nil {
				return
			}
		}
		_, _ = conn.Write([]byte(reply))
	}()

	return ln.Addr().String()
}

func TestScanReader_Clean(t *testing.T) {
	addr := fakeClamd(t, "stream: OK\x00")
	client := New(Config{Address: addr, Timeout: 2 * time.Second})

	res, err := client.ScanReader(context.Background(), bytes.NewReader([]byte("hello world")))
	require.NoError(t, err)
	require.False(t, res.Infected)
}

func TestScanReader_Infected(t *testing.T) {
	addr := fakeClamd(t, "stream: Eicar-Test-Signature FOUND\x00")
	client := New(Config{Address: addr, Timeout: 2 * time.Second})

	res, err := client.ScanReader(context.Background(), bytes.NewReader([]byte("X5O!P%@AP")))
	require.NoError(t, err)
	require.True(t, res.Infected)
	require.Equal(t, "Eicar-Test-Signature", res.Signature)
}

func TestScanReader_LargeInput(t *testing.T) {
	addr := fakeClamd(t, "stream: OK\x00")
	client := New(Config{Address: addr, Timeout: 5 * time.Second})

	data := bytes.Repeat([]byte{0xAB}, chunkSize*3+17) // несколько чанков + хвост
	res, err := client.ScanReader(context.Background(), bytes.NewReader(data))
	require.NoError(t, err)
	require.False(t, res.Infected)
}

func TestHealthCheck(t *testing.T) {
	addr := fakeClamd(t, "")
	client := New(Config{Address: addr})

	require.NoError(t, client.HealthCheck(context.Background()))
}
