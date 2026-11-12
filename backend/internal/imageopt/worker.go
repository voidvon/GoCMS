package imageopt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"time"
)

const (
	workerArgument = "__gocms-image-worker"
	processTimeout = 30 * time.Second
	queueTimeout   = 5 * time.Second
	workerHeapExit = 78
)

var (
	ErrBusy     = errors.New("图片处理繁忙，请稍后重试")
	ErrTooLarge = errors.New("图片不能超过 10 MB")
	ErrWorker   = errors.New("图片处理服务暂时不可用，请稍后重试")
	processing  = make(chan struct{}, 1)
	admission   = make(chan struct{}, 5) // One worker and at most four waiting jobs.
)

// Optimize validates and processes an upload in a disposable copy of the current
// executable. The entry point must call RunWorker before starting the server.
// Killing and waiting for this child makes cancellation effective inside codecs
// that do not accept a context, and releases the processing slot only after exit.
func Optimize(ctx context.Context, data []byte, options Config) (Result, error) {
	return run(ctx, data, options, processTimeout, queueTimeout)
}

func run(ctx context.Context, data []byte, options Config, timeout, wait time.Duration) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(data) > MaxBytes {
		return Result{}, ErrTooLarge
	}
	select {
	case admission <- struct{}{}:
		defer func() { <-admission }()
	default:
		return Result{}, ErrBusy
	}
	queued, cancelQueue := context.WithTimeout(ctx, wait)
	defer cancelQueue()
	select {
	case processing <- struct{}{}:
		defer func() { <-processing }()
	case <-queued.Done():
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		return Result{}, ErrBusy
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if queued.Err() != nil {
		return Result{}, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		return Result{}, ErrWorker
	}
	header, _ := json.Marshal(options)
	cmd := exec.CommandContext(ctx, executable, workerArgument)
	cmd.Stdin = io.MultiReader(bytes.NewReader(append(header, '\n')), bytes.NewReader(data))
	output := limitedBuffer{limit: MaxBytes + 4096}
	cmd.Stdout = &output
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("图片处理已取消或超时，请缩小图片后重试: %w", ctx.Err())
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == workerHeapExit {
			return Result{}, fmt.Errorf("图片处理占用内存过大，请缩小图片后重试")
		}
		return Result{}, ErrWorker
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	reader := bufio.NewReader(bytes.NewReader(output.Bytes()))
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return Result{}, ErrWorker
	}
	var response workerResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return Result{}, ErrWorker
	}
	if response.Error != "" {
		if response.TooLarge {
			return Result{}, ErrTooLarge
		}
		return Result{}, errors.New(response.Error)
	}
	result := response.Result
	result.Data, err = io.ReadAll(reader)
	if err != nil || len(result.Data) == 0 || len(result.Data) > MaxBytes || !validDimensions(result.Width, result.Height) {
		return Result{}, ErrWorker
	}
	switch result.Format {
	case "jpeg", "png", "gif", "webp":
		return result, nil
	default:
		return Result{}, ErrWorker
	}
}

type workerResponse struct {
	Result   Result
	Error    string
	TooLarge bool
}

// RunWorker handles only the private worker invocation; it never opens the site
// database or starts an HTTP listener. stdin/stdout carry bounded image data.
func RunWorker() bool {
	if len(os.Args) != 2 || os.Args[1] != workerArgument {
		return false
	}
	runtime.GOMAXPROCS(1)
	debug.SetMemoryLimit(512 << 20)
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			if stats.HeapAlloc > 768<<20 {
				os.Exit(workerHeapExit)
			}
		}
	}()
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadSlice('\n')
	var options Config
	if err == nil {
		err = json.Unmarshal(line, &options)
	}
	var result Result
	if err == nil {
		var data []byte
		data, err = io.ReadAll(io.LimitReader(reader, MaxBytes+1))
		if err == nil {
			result, err = optimize(data, options)
		}
	}
	response := workerResponse{Result: result}
	data := result.Data
	response.Result.Data = nil
	if err != nil {
		response.Error = err.Error()
		response.TooLarge = errors.Is(err, ErrTooLarge)
	}
	if json.NewEncoder(os.Stdout).Encode(response) == nil && err == nil {
		_, _ = os.Stdout.Write(data)
	}
	return true
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Len() int      { return b.buffer.Len() }
func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, ErrTooLarge
	}
	return b.buffer.Write(p)
}
