// Package clamav — минимальный клиент протокола ClamAV INSTREAM (без внешних зависимостей), используется
// при confirm файлов (backend-plan.md §8.1, §12.11): один проход по потоку файла — sha256 + скан на вирусы.
package clamav

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type Config struct {
	Address string // host:port, напр. clamav:3310 (infra/docker-compose.yml)
	Timeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	return c
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg.withDefaults()}
}

type Result struct {
	Infected  bool
	Signature string
}

const chunkSize = 64 * 1024

// ScanReader сканирует поток по протоколу INSTREAM: 4-байтовые big-endian чанки, завершение —
// чанк нулевой длины. Один проход по r — вызывающий код может обернуть исходный поток TeeReader'ом,
// чтобы одновременно считать sha256 (backend-plan.md §8.1: "одним потоком из MinIO").
func (c *Client) ScanReader(ctx context.Context, r io.Reader) (Result, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Address)
	if err != nil {
		return Result{}, fmt.Errorf("connect clamav: %w", err)
	}
	defer func() { _ = conn.Close() }()

	deadline := time.Now().Add(c.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return Result{}, fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return Result{}, fmt.Errorf("send INSTREAM command: %w", err)
	}

	buf := make([]byte, chunkSize)
	lenPrefix := make([]byte, 4)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(lenPrefix, uint32(n))
			if _, err := conn.Write(lenPrefix); err != nil {
				return Result{}, fmt.Errorf("write chunk length: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return Result{}, fmt.Errorf("write chunk: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return Result{}, fmt.Errorf("read source: %w", readErr)
		}
	}

	// Завершающий нулевой чанк.
	binary.BigEndian.PutUint32(lenPrefix, 0)
	if _, err := conn.Write(lenPrefix); err != nil {
		return Result{}, fmt.Errorf("write terminating chunk: %w", err)
	}

	reply, err := bufio.NewReader(conn).ReadString(0)
	if err != nil && !strings.Contains(reply, "OK") && !strings.Contains(reply, "FOUND") {
		return Result{}, fmt.Errorf("read clamav response: %w", err)
	}
	return parseReply(reply), nil
}

func parseReply(reply string) Result {
	reply = strings.TrimRight(reply, "\x00\r\n ")
	// Успех: "stream: OK". Вирус: "stream: <Signature> FOUND". Ошибка: "... ERROR".
	if strings.HasSuffix(reply, "FOUND") {
		sig := strings.TrimSuffix(reply, "FOUND")
		sig = strings.TrimSpace(strings.TrimPrefix(sig, "stream:"))
		return Result{Infected: true, Signature: sig}
	}
	return Result{Infected: false}
}

// HealthCheck — используется в /readyz.
func (c *Client) HealthCheck(ctx context.Context) error {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Address)
	if err != nil {
		return fmt.Errorf("clamav not responding: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte("zPING\x00")); err != nil {
		return fmt.Errorf("clamav ping: %w", err)
	}
	reply, err := bufio.NewReader(conn).ReadString(0)
	if err != nil {
		return fmt.Errorf("clamav ping response: %w", err)
	}
	if !strings.Contains(reply, "PONG") {
		return fmt.Errorf("clamav unexpected ping response: %q", reply)
	}
	return nil
}
