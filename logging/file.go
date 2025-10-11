// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package logging provides utilities for logging in bandwidth estimation tests.
package logging

import (
	"bufio"
	"io"
	"os"
)

// GetLogFile returns an io.WriteCloser for the specified file path.
// If file is empty, it returns a no-op writer.
// If file is "stdout", it returns os.Stdout wrapped in a nopCloser.
// Otherwise, it creates and returns the specified file.
func GetLogFile(file string) (io.WriteCloser, error) {
	if len(file) == 0 {
		return nopCloser{io.Discard}, nil
	}
	if file == "stdout" {
		return nopCloser{os.Stdout}, nil
	}
	//nolint:gosec
	fd, err := os.Create(file)
	if err != nil {
		return nil, err
	}
	bufwriter := bufio.NewWriterSize(fd, 4096)

	return &fileCloser{
		f:   fd,
		buf: bufwriter,
	}, nil
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

type fileCloser struct {
	f   *os.File
	buf *bufio.Writer
}

func (f *fileCloser) Write(buf []byte) (int, error) {
	return f.f.Write(buf)
}

func (f *fileCloser) Close() error {
	if err := f.buf.Flush(); err != nil {
		return err
	}

	return f.f.Close()
}
