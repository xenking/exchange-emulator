package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xenking/zipstream"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Println("Usage: go run get_data.go <start-date> <end-date> <pair> <out>.csv")
		fmt.Println("Example: go run get_data.go 2022-01-01 2022-08-27 ETHUSDT ETHUSDT-1m.csv")
		os.Exit(1)
	}

	dateStart, _ := time.Parse("2006-01-02", os.Args[1])
	dateEnd, _ := time.Parse("2006-01-02", os.Args[2])

	dateStart = time.Date(dateStart.Year(), dateStart.Month(), dateStart.Day(), 0, 0, 0, 0, time.UTC)
	dateEnd = time.Date(dateEnd.Year(), dateEnd.Month(), dateEnd.Day(), 0, 0, 0, 0, time.UTC)

	// Create the file
	r, w := io.Pipe()

	go func() {
		out, err := os.Create(os.Args[4])
		if err != nil {
			panic(err)
		}
		defer out.Close()
		// write header
		out.WriteString("unix,date,symbol,open,high,low,close,Volume ETH,Volume USDT,tradecount\n")

		err = removeUnusedData(os.Args[3][:3]+"/"+os.Args[3][3:], r, out)
	}()

	for d := dateStart; d.Before(dateEnd); d = d.AddDate(0, 0, 1) {
		url := fmt.Sprintf("https://data.binance.vision/data/spot/daily/klines/%s/1m/%s-1m-%s.zip", os.Args[3], os.Args[3],
			d.Format("2006-01-02"))
		fmt.Println("Downloading", os.Args[3], d.Format("2006-01-02"))
		err := downloadFile(url, w)
		if err != nil {
			panic(err)
		}
	}

	err := w.Close()
	if err != nil {
		panic(err)
	}
}

func downloadFile(url string, file io.Writer) (err error) {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	zr := zipstream.NewReader(resp.Body)
	_, err = zr.Next()
	if err != nil {
		if err != io.EOF {
			return err
		}
	}
	_, err = io.Copy(file, zr)

	if err != nil {
		return err
	}

	return nil
}

const defaultLayout = "2006-01-02T15:04:05Z"

func removeUnusedData(pair string, in io.Reader, out *os.File) error {
	var sb strings.Builder
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		sb.Reset()
		fields := strings.Split(sc.Text(), ",")

		ts, err := strconv.Atoi(fields[0])
		if err != nil {
			return err
		}

		sb.WriteString(fields[0])
		sb.WriteByte(',')
		sb.WriteString(time.Unix(int64(ts/1000), 0).Format(defaultLayout))
		sb.WriteByte(',')
		sb.WriteString(pair)
		sb.WriteByte(',')
		sb.WriteString(strings.Join(fields[1:6], ","))
		sb.WriteByte(',')
		sb.WriteString(strings.Join(fields[7:9], ","))
		sb.WriteByte('\n')

		_, err = out.WriteString(sb.String())
		if err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	return nil
}
