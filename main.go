package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

type Codec string

const (
	AV1  Codec = "AV1"
	H264 Codec = "H264"
	H265 Codec = "H265"
)

var validCodecs = map[string]Codec{
	"h264": H264,
	"h265": H265,
	"av1":  AV1,
}

func main() {
	clipsFolder := flag.String("folder", `E:\Clips\2026\October\Overwatch Application`, "folder containing the clips")
	yes := flag.Bool("yes", false, "skip confirmation prompt")
	flag.Parse()

	if err := run(*clipsFolder, *yes); err != nil {
		log.Fatal(err)
	}
}

func run(clipsFolder string, skipConfirmation bool) error {
	fmt.Println("starting")
	if !checkHasFFProbeInstalled() {
		return errors.New("ffprobe not installed")
	}

	entries, err := os.ReadDir(clipsFolder)
	if err != nil {
		return err
	}

	clipsByCodec := map[Codec][]string{
		H264: {},
		H265: {},
		AV1:  {},
	}

	var g errgroup.Group
	var mu sync.Mutex

	g.SetLimit(12)
	for _, clip := range entries {
		clipName := clip.Name()
		if clip.IsDir() || !isSupportedExt(clipName) {
			continue
		}

		g.Go(func() error {

			codec, err := checkEncodeCodec(filepath.Join(clipsFolder, clipName))
			if err != nil {
				return err
			}
			mu.Lock()
			clipsByCodec[codec] = append(clipsByCodec[codec], clipName)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		log.Fatalf("error happened: %v\n", err)
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		selected, err := promptForActionOnClips(reader, clipsFolder, clipsByCodec)
		for err != nil {
			fmt.Println("sorry, there was an issue with ur answer")
			selected, err = promptForActionOnClips(reader, clipsFolder, clipsByCodec)
		}

		if !skipConfirmation {
			fmt.Printf("\nis %v correct? y/n\n", selected)
			confirmation, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(confirmation)) != "y" {
				fmt.Println("okay, pick again")
				continue
			}
		}

		for _, c := range selected {
			if len(clipsByCodec[c]) == 0 {
				continue
			}
			if err := sendToRecycleBin(clipsFolder, clipsByCodec[c]); err != nil {
				log.Printf("failed deleting %s clips: %v", c, err)
			} else {
				fmt.Printf("sent %d %s clips to the recycle bin\n", len(clipsByCodec[c]), c)
			}
		}
		return nil
	}
}

func isSupportedExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".mov":
		return true
	}
	return false
}

func formatSize(bytes int64) string {
	const mb = 1 << 20
	if bytes >= mb {
		return fmt.Sprintf("%.2f MB", float64(bytes)/mb)
	}
	return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
}

func codecSize(clipsFolder string, clips []string) int64 {
	var total int64
	for _, clip := range clips {
		info, err := os.Stat(filepath.Join(clipsFolder, clip))
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

func promptForActionOnClips(reader *bufio.Reader, clipsFolder string, clipsByCodec map[Codec][]string) ([]Codec, error) {
	fmt.Printf("all clips collected, which to delete:\n")
	for _, c := range []Codec{H264, H265, AV1} {
		count := len(clipsByCodec[c])
		if count == 0 {
			fmt.Printf("%s: 0 (none found)\n", strings.ToLower(string(c)))
		} else {
			fmt.Printf("%s: %d (%s)\n", strings.ToLower(string(c)), count, formatSize(codecSize(clipsFolder, clipsByCodec[c])))
		}
	}
	fmt.Printf("\nuse commas to select multiple e.g h264,av1\n")
	answer, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return parseCodecs(answer)
}

func parseCodecs(answer string) ([]Codec, error) {
	parts := strings.Split(answer, ",")
	var result []Codec
	seen := map[Codec]bool{}
	for _, p := range parts {
		c, ok := validCodecs[strings.ToLower(strings.TrimSpace(p))]
		if !ok || seen[c] {
			return nil, fmt.Errorf("invalid input: %s", p)
		}
		seen[c] = true
		result = append(result, c)
	}
	if len(result) == 0 {
		return nil, errors.New("no codecs selected")
	}
	return result, nil
}

func sendToRecycleBin(clipsFolder string, clips []string) error {
	if len(clips) == 0 {
		return nil
	}
	var script strings.Builder
	script.WriteString("Add-Type -AssemblyName Microsoft.VisualBasic; ")
	for _, clip := range clips {
		script.WriteString(fmt.Sprintf("[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile(%q, 'OnlyErrorDialogs', 'SendToRecycleBin'); ", filepath.Join(clipsFolder, clip)))
	}
	return exec.Command("powershell", "-NoProfile", "-Command", script.String()).Run()
}

func checkHasFFProbeInstalled() bool {
	out, err := exec.Command("ffprobe", "-version").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "ffprobe version")
}

func checkEncodeCodec(clip string) (Codec, error) {
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name", "-of", "csv=print_section=0", clip).Output()

	if err != nil {
		return "", err
	}

	outputString := strings.ToLower(strings.TrimSpace(string(out)))

	if outputString == strings.ToLower(string(AV1)) {
		return AV1, nil
	} else if outputString == strings.ToLower(string(H264)) {
		return H264, nil
	} else if outputString == strings.ToLower(string(H265)) {
		return H265, nil
	}

	return "", fmt.Errorf("unknown codec: %s", outputString)
}
