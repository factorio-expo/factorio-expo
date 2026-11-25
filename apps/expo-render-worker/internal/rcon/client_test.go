package rcon

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	gorcon "github.com/gorcon/rcon"
)

func TestExecuteHonorsCancellationAndDiscardsConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	// Authentication succeeds, but the command's response never arrives.
	go func() {
		var size int32
		if binary.Read(server, binary.LittleEndian, &size) != nil {
			return
		}
		packet := make([]byte, size)
		if _, err := io.ReadFull(server, packet); err != nil {
			return
		}
		response := make([]byte, 14)
		binary.LittleEndian.PutUint32(response[0:4], 10)
		copy(response[4:8], packet[:4])
		binary.LittleEndian.PutUint32(response[8:12], 2)
		_, _ = server.Write(response)
		_, _ = io.Copy(io.Discard, server)
	}()
	c := NewClient("", "", time.Second)
	// Seed the authenticated connection to isolate Execute's context handling.
	conn, err := gorcon.Open(client, "", gorcon.SetDeadline(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	c.conn, c.rawConn = conn, client
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.Execute(ctx, "/bp-preview-status")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("Execute ignored context deadline")
	}
	if c.conn != nil || c.rawConn != nil {
		t.Fatal("canceled connection was retained")
	}
}
