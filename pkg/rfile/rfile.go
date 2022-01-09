package rfile

import (
	"bytes"
	"io"
	"os"
)

// Rfile manages a file opened for reading line-by-line in reverse.
type Rfile struct {
	fh       *os.File
	offset   int64
	bufsize  int
	lines    [][]byte
	i        int
	lastLine []byte
}

// Open returns Rfile handle to be read in reverse line-by-line.
func Open(file string) (*Rfile, error) {
	var err error
	rf := new(Rfile)
	if rf.fh, err = os.Open(file); err != nil {
		return nil, err
	}
	fi, _ := rf.fh.Stat()
	rf.offset = fi.Size()
	rf.bufsize = 4096

	return rf, nil
}

// Close file that was opened.
func (rf *Rfile) Close() error {
	rf.lastLine = nil
	rf.lines = nil

	return rf.fh.Close()
}

var nlDelim = []byte("\n")

// ReadLine returns the  next previous line, beginning with the last line in the file.
// When the beginning of the file is reached: "", io.EOF is returned.
func (rf *Rfile) ReadLine() ([]byte, error) {
	if rf.i > 0 {
		rf.i--

		return rf.lines[rf.i+1], nil
	}
	if rf.i < 0 {
		return nil, io.EOF
	}
	if rf.offset == 0 {
		rf.i-- // use as flag to send EOF on next call

		return rf.lines[0], nil
	}

	// get more from file - back up from end-of-file
	rf.offset -= int64(rf.bufsize)
	if rf.offset < 0 {
		rf.bufsize += int(rf.offset) // rf.offset is negative
		rf.offset = 0
	}
	_, err := rf.fh.Seek(rf.offset, 0)
	if err != nil {
		return nil, err
	}

	// size buffer
	buf := make([]byte, rf.bufsize)
	if n, err := rf.fh.Read(buf); err != nil && err != io.EOF {
		return nil, err
	} else if n != rf.bufsize { // shouldn't happen
		buf = buf[:n]
	}

	// get the lines in the buffer, append what was carried over
	//nolint:makezero
	if len(rf.lines) > 0 {
		buf = append(buf, rf.lines[0]...)
	}
	rf.lines = bytes.SplitAfter(buf, nlDelim)
	rf.i = len(rf.lines) - 1

	return rf.ReadLine() // now read the next line back
}

func (rf *Rfile) Read(buf []byte) (n int, err error) {
	if len(rf.lastLine) > 0 {
		n = copy(buf, rf.lastLine)
		if n < len(rf.lastLine) {
			rf.lastLine = rf.lastLine[n:]

			return n, nil
		}
		rf.lastLine = nil
	}
	remain := len(buf) - n
	if remain == 0 {
		return n, nil
	}

	var line []byte
	var nn int
	for remain > 0 {
		line, err = rf.ReadLine()
		if err != nil {
			return
		}
		if len(line) == 0 {
			return n, nil
		}
		nn = copy(buf[n:], line)
		remain -= nn
		n += nn
	}
	if nn < len(line) {
		rf.lastLine = line[nn:]
	}

	return n, err
}
