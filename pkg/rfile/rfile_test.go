package rfile_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/xenking/exchange-emulator/pkg/rfile"
)

func TestRfile_ReadLine(t *testing.T) {
	rf, err := rfile.Open("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	for {
		line, err := rf.ReadLine()
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		fmt.Print(string(line))
	}
}

func TestRfile_Read(t *testing.T) {
	rf, err := rfile.Open("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	all, err := io.ReadAll(rf)
	if err != nil {
		return
	}
	if len(all) == 0 {
		t.Error("empty read")
	}
	idx := bytes.IndexByte(all, '\n')
	fmt.Println(string(all[idx-1 : idx+1]))
	fmt.Printf(string(all))
}
