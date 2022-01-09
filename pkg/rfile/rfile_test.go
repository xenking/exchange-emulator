package rfile

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

func TestRfile_ReadLine(t *testing.T) {
	rf, err := Open("testdata")
	if err != nil {
		t.Fatal(err)
	}
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
	rf.Close()
}

func TestRfile_Read(t *testing.T) {
	rf, err := Open("testdata")
	if err != nil {
		t.Fatal(err)
	}
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
	rf.Close()
}
