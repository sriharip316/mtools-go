package logfile

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sriharip316/mtools-go/internal/logevent"
)

// LogFile represents a MongoDB LogV2 log file or input stream.
type LogFile struct {
	Name       string
	file       *os.File
	reader     *bufio.Reader
	Start      time.Time
	End        time.Time
	FileSize   int64
	isSeekable bool
}

// Open opens a log file on disk and calculates start/end timestamp bounds.
func Open(path string) (*LogFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	lf := &LogFile{
		Name:       path,
		file:       f,
		reader:     bufio.NewReaderSize(f, 64*1024),
		FileSize:   info.Size(),
		isSeekable: true,
	}

	if err := lf.calculateBounds(); err != nil {
		_ = f.Close()
		return nil, err
	}

	return lf, nil
}

// NewFromReader creates a LogFile from an io.Reader (e.g. os.Stdin).
// Such LogFiles are not seekable and bounds are not calculated upfront.
func NewFromReader(r io.Reader, name string) *LogFile {
	return &LogFile{
		Name:       name,
		reader:     bufio.NewReaderSize(r, 64*1024),
		isSeekable: false,
	}
}

// Next reads and parses the next LogEvent from the stream.
// Returns io.EOF when the end of the file/stream is reached.
func (lf *LogFile) Next() (*logevent.LogEvent, error) {
	for {
		line, err := lf.reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if len(line) == 0 {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}

		ev, parseErr := logevent.Parse(line)
		if parseErr != nil {
			// If a line is not valid LogV2 JSON, skip it or try next line
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}

		return ev, nil
	}
}

// FastForward seeks the file pointer to the first event with datetime >= target.
func (lf *LogFile) FastForward(target time.Time) {
	if !lf.isSeekable || target.IsZero() || lf.FileSize == 0 {
		return
	}

	if !lf.Start.IsZero() && !target.After(lf.Start) {
		_, _ = lf.file.Seek(0, io.SeekStart)
		lf.reader.Reset(lf.file)
		return
	}

	low := int64(0)
	high := lf.FileSize

	for high-low > 4096 {
		mid := low + (high-low)/2
		_, _ = lf.file.Seek(mid, io.SeekStart)
		lf.reader.Reset(lf.file)

		// Discard partial line
		_, _ = lf.reader.ReadString('\n')

		ev, err := lf.Next()
		if err != nil {
			// Near end of file
			high = mid
			continue
		}

		if ev.DateTime.Before(target) {
			low = mid
		} else {
			high = mid
		}
	}

	// Linear scan from 'low' to find the exact first line >= target
	_, _ = lf.file.Seek(low, io.SeekStart)
	lf.reader.Reset(lf.file)
	if low > 0 {
		// Discard partial line if we seeked to mid-file
		_, _ = lf.reader.ReadString('\n')
	}

	for {
		// Record current position before reading
		offset, err := lf.file.Seek(0, io.SeekCurrent)
		if err != nil {
			break
		}
		// Account for buffered unread bytes in bufio.Reader
		buffered := int64(lf.reader.Buffered())
		linePos := offset - buffered

		ev, err := lf.Next()
		if err != nil {
			break
		}

		if !ev.DateTime.Before(target) {
			// Found first line >= target! Seek directly to its start
			_, _ = lf.file.Seek(linePos, io.SeekStart)
			lf.reader.Reset(lf.file)
			return
		}
	}
}

// calculateBounds scans start and end of file to find timestamps.
func (lf *LogFile) calculateBounds() error {
	if lf.FileSize == 0 {
		return nil
	}

	// 1. Find Start timestamp from beginning
	if _, err := lf.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	lf.reader.Reset(lf.file)

	for range 50 {
		ev, err := lf.Next()
		if err != nil {
			break
		}
		if !ev.DateTime.IsZero() {
			lf.Start = ev.DateTime
			break
		}
	}

	// 2. Find End timestamp from near end of file
	tailSize := min(int64(64*1024), lf.FileSize)

	if _, err := lf.file.Seek(lf.FileSize-tailSize, io.SeekStart); err != nil {
		return err
	}
	lf.reader.Reset(lf.file)

	// Discard first partial line if not at file start
	if lf.FileSize-tailSize > 0 {
		_, _ = lf.reader.ReadString('\n')
	}

	var lastDT time.Time
	for {
		ev, err := lf.Next()
		if err != nil {
			break
		}
		if !ev.DateTime.IsZero() {
			lastDT = ev.DateTime
		}
	}
	lf.End = lastDT

	// Reset reader to beginning
	if _, err := lf.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	lf.reader.Reset(lf.file)
	return nil
}

// Rewind resets the log file reading position back to the beginning of the file.
func (lf *LogFile) Rewind() error {
	if !lf.isSeekable || lf.file == nil {
		return nil
	}
	if _, err := lf.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	lf.reader.Reset(lf.file)
	return nil
}

// Close closes the underlying file descriptor if open.
func (lf *LogFile) Close() error {
	if lf.file != nil {
		return lf.file.Close()
	}
	return nil
}
