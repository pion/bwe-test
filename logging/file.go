// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package logging

import (
	"bufio"
	"io"
	"os"
)

func GetLogFile(file string) (io.WriteCloser, error) {
	if len(file) == 0 {
		return nopCloser{io.Discard}, nil
	}
	if file == "stdout" {
		return nopCloser{os.Stdout}, nil
	}
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
