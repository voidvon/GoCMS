package site

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	updateRepository      = "voidvon/GoCMS"
	maxUpdateArchiveBytes = 512 << 20
)

var errNoPublishedRelease = errors.New("暂无已发布的 GitHub Release")

type githubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Body    string               `json:"body"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type updateVersion struct {
	Major int
	Minor int
	Patch int
}

type updateCheckResponse struct {
	CurrentVersion   string `json:"current_version"`
	LatestVersion    string `json:"latest_version"`
	LatestTag        string `json:"latest_tag"`
	UpdateAvailable  bool   `json:"update_available"`
	CanUpdate        bool   `json:"can_update"`
	ReleaseAvailable bool   `json:"release_available"`
	AssetName        string `json:"asset_name"`
	ReleaseURL       string `json:"release_url"`
	ReleaseNotes     string `json:"release_notes"`
}

type updateRequest struct {
	CurrentVersion string `json:"current_version"`
}

func (s *Server) adminUpdateCheck(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || !s.requireAdmin(response, request) {
		return
	}
	currentVersion := strings.TrimSpace(request.URL.Query().Get("current_version"))
	if _, err := parseUpdateVersion(currentVersion); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "版本号无效"})
		return
	}
	release, err := fetchLatestRelease(request.Context())
	if err != nil {
		if errors.Is(err, errNoPublishedRelease) {
			writeJSON(response, http.StatusOK, updateCheckResponse{
				CurrentVersion:   currentVersion,
				LatestVersion:    currentVersion,
				ReleaseAvailable: false,
			})
			return
		}
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("检查更新失败：%v", err)})
		return
	}
	writeJSON(response, http.StatusOK, buildUpdateCheck(currentVersion, release))
}

func (s *Server) adminUpdate(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || !s.requireAdmin(response, request) {
		return
	}
	var payload updateRequest
	if err := decodeRequest(request, &payload); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "更新请求无效"})
		return
	}
	currentVersion := strings.TrimSpace(payload.CurrentVersion)
	if _, err := parseUpdateVersion(currentVersion); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "版本号无效"})
		return
	}
	release, err := fetchLatestRelease(request.Context())
	if err != nil {
		if errors.Is(err, errNoPublishedRelease) {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "当前没有可用的 GitHub Release"})
			return
		}
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("获取更新失败：%v", err)})
		return
	}
	check := buildUpdateCheck(currentVersion, release)
	if !check.UpdateAvailable {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "当前已经是最新版本"})
		return
	}
	if !check.CanUpdate {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "当前平台没有可用的更新包"})
		return
	}

	s.updateMu.Lock()
	if s.updateActive {
		s.updateMu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "更新已经在进行中"})
		return
	}
	s.updateActive = true
	s.updateMu.Unlock()

	staging, err := downloadAndExtractUpdate(request.Context(), release, check.AssetName)
	if err != nil {
		s.resetUpdateState()
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("下载更新失败：%v", err)})
		return
	}
	if err := launchUpdate(staging); err != nil {
		_ = os.RemoveAll(filepath.Dir(staging))
		s.resetUpdateState()
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("启动更新失败：%v", err)})
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{
		"ok":      true,
		"version": check.LatestVersion,
		"message": "更新已开始，程序将自动重启",
	})
}

func (s *Server) resetUpdateState() {
	s.updateMu.Lock()
	s.updateActive = false
	s.updateMu.Unlock()
}

func parseUpdateVersion(value string) (updateVersion, error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	if len(parts) != 3 {
		return updateVersion{}, fmt.Errorf("版本号格式无效")
	}
	values := make([]int, 3)
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return updateVersion{}, fmt.Errorf("版本号格式无效")
		}
		values[index] = parsed
	}
	return updateVersion{Major: values[0], Minor: values[1], Patch: values[2]}, nil
}

func compareUpdateVersions(left, right updateVersion) int {
	for _, pair := range [][2]int{{left.Major, right.Major}, {left.Minor, right.Minor}, {left.Patch, right.Patch}} {
		if pair[0] > pair[1] {
			return 1
		}
		if pair[0] < pair[1] {
			return -1
		}
	}
	return 0
}

func formatUpdateVersion(value updateVersion) string {
	return fmt.Sprintf("%d.%d.%d", value.Major, value.Minor, value.Patch)
}

func releaseVersion(tag string) (updateVersion, string, error) {
	version, err := parseUpdateVersion(tag)
	if err != nil {
		return updateVersion{}, "", err
	}
	return version, formatUpdateVersion(version), nil
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+updateRepository+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "GoCMS-updater")
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if response.StatusCode == http.StatusNotFound {
			return githubRelease{}, errNoPublishedRelease
		}
		return githubRelease{}, fmt.Errorf("GitHub 返回 HTTP %d", response.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if _, _, err := releaseVersion(release.TagName); err != nil {
		return githubRelease{}, fmt.Errorf("GitHub Release tag %q 无效", release.TagName)
	}
	return release, nil
}

func updateAssetName(tag string) string {
	return fmt.Sprintf("gocms-%s-%s-%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH)
}

func releaseAsset(release githubRelease, name string) (githubReleaseAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

func buildUpdateCheck(current string, release githubRelease) updateCheckResponse {
	currentVersion, _ := parseUpdateVersion(current)
	latestVersion, latest, _ := releaseVersion(release.TagName)
	assetName := updateAssetName(release.TagName)
	_, assetAvailable := releaseAsset(release, assetName)
	return updateCheckResponse{
		CurrentVersion:   current,
		LatestVersion:    latest,
		LatestTag:        release.TagName,
		UpdateAvailable:  compareUpdateVersions(latestVersion, currentVersion) > 0,
		CanUpdate:        assetAvailable,
		ReleaseAvailable: true,
		AssetName:        assetName,
		ReleaseURL:       release.HTMLURL,
		ReleaseNotes:     release.Body,
	}
}

func downloadAndExtractUpdate(ctx context.Context, release githubRelease, assetName string) (string, error) {
	asset, ok := releaseAsset(release, assetName)
	if !ok {
		return "", fmt.Errorf("没有找到 %s", assetName)
	}
	downloadURL, err := url.Parse(asset.BrowserDownloadURL)
	if err != nil {
		return "", errors.New("更新地址不是受信任的 GitHub HTTPS 地址")
	}
	hostname := downloadURL.Hostname()
	if downloadURL.Scheme != "https" || (hostname != "github.com" && !strings.HasSuffix(hostname, ".github.com")) {
		return "", errors.New("更新地址不是受信任的 GitHub HTTPS 地址")
	}
	temporaryRoot, err := os.MkdirTemp("", "gocms-update-")
	if err != nil {
		return "", err
	}
	archivePath := filepath.Join(temporaryRoot, assetName)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "GoCMS-updater")
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = os.RemoveAll(temporaryRoot)
		return "", fmt.Errorf("下载更新返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxUpdateArchiveBytes {
		_ = os.RemoveAll(temporaryRoot)
		return "", errors.New("更新包超过大小限制")
	}
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", err
	}
	written, copyErr := io.Copy(archive, io.LimitReader(response.Body, maxUpdateArchiveBytes+1))
	closeErr := archive.Close()
	if copyErr != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", closeErr
	}
	if written > maxUpdateArchiveBytes {
		_ = os.RemoveAll(temporaryRoot)
		return "", errors.New("更新包超过大小限制")
	}

	staging := filepath.Join(temporaryRoot, "stage")
	if err := os.Mkdir(staging, 0700); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", err
	}
	if err := extractUpdateArchive(archivePath, staging); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", err
	}
	if err := validateStagedUpdate(staging); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return "", err
	}
	return staging, nil
}

func currentBinaryName() string {
	if runtime.GOOS == "windows" {
		return "site.exe"
	}
	return "site"
}

func isAllowedUpdatePath(name string) bool {
	if name == "bin" || name == filepath.ToSlash(filepath.Join("bin", currentBinaryName())) {
		return true
	}
	for _, directory := range []string{"frontend", "backend"} {
		if name == directory {
			return true
		}
	}
	for _, prefix := range []string{"frontend/dist", "backend/templates"} {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	return false
}

func extractUpdateArchive(archivePath, destinationRoot string) error {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archiveFile.Close()
	compressed, err := gzip.NewReader(archiveFile)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := make(map[string]bool)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := header.Name
		if name == "" || strings.Contains(name, "\\") {
			return errors.New("更新包包含无效路径")
		}
		cleanName := path.Clean(name)
		if cleanName != name || strings.HasPrefix(cleanName, "/") || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
			return errors.New("更新包包含路径穿越")
		}
		if !isAllowedUpdatePath(cleanName) {
			return fmt.Errorf("更新包包含不允许的文件：%s", name)
		}
		if seen[cleanName] {
			return fmt.Errorf("更新包包含重复文件：%s", cleanName)
		}
		seen[cleanName] = true
		destination := filepath.Join(destinationRoot, filepath.FromSlash(cleanName))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(destination, 0700); err != nil {
				return err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("更新包包含不支持的文件类型：%s", cleanName)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(destination, header.FileInfo().Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func validateStagedUpdate(staging string) error {
	paths := []string{
		filepath.Join(staging, "bin", currentBinaryName()),
		filepath.Join(staging, "frontend", "dist", "index.html"),
		filepath.Join(staging, "backend", "templates", "index.html"),
	}
	for _, candidate := range paths {
		info, err := os.Stat(candidate)
		if err != nil {
			return fmt.Errorf("更新包缺少 %s", candidate)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("更新包中的 %s 不是普通文件", candidate)
		}
	}
	return nil
}

func resolveExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	return filepath.Abs(executable)
}

func launchUpdate(staging string) error {
	executable, err := resolveExecutable()
	if err != nil {
		return err
	}
	if filepath.Base(filepath.Dir(executable)) != "bin" {
		return errors.New("当前程序不是从 bin/site 启动，无法自动更新")
	}
	arguments := []string{"__apply-update", "--parent-pid", strconv.Itoa(os.Getpid()), "--target", executable, "--staging", staging, "--"}
	arguments = append(arguments, os.Args[1:]...)
	command := exec.Command(executable, arguments...)
	command.Dir, _ = os.Getwd()
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func ApplyUpdate(arguments []string) error {
	flags := flag.NewFlagSet("__apply-update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	parentPID := flags.Int("parent-pid", 0, "旧进程 PID")
	target := flags.String("target", "", "程序路径")
	staging := flags.String("staging", "", "更新暂存目录")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *parentPID <= 0 || *target == "" || *staging == "" {
		return errors.New("更新参数不完整")
	}
	if err := waitForProcessExit(*parentPID, 30*time.Second); err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(*staging))
	if err := applyStagedUpdate(*staging, *target); err != nil {
		return err
	}
	command := exec.Command(*target, flags.Args()...)
	command.Dir, _ = os.Getwd()
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func applyStagedUpdate(staging, target string) error {
	if filepath.Base(filepath.Dir(target)) != "bin" {
		return errors.New("目标程序不是 bin/site")
	}
	installRoot := filepath.Dir(filepath.Dir(target))
	for _, directory := range []string{
		filepath.Join("frontend", "dist"),
		filepath.Join("backend", "templates"),
		filepath.Join("assets", "theme", "blue"),
	} {
		if err := replaceUpdateDirectory(filepath.Join(staging, directory), filepath.Join(installRoot, directory)); err != nil {
			return err
		}
	}
	return replaceUpdateFile(filepath.Join(staging, "bin", currentBinaryName()), target, 0755)
}

func copyUpdateFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func replaceUpdateFile(source, destination string, mode os.FileMode) error {
	temporary := fmt.Sprintf("%s.update-%d", destination, os.Getpid())
	_ = os.Remove(temporary)
	if err := copyUpdateFile(source, temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func copyUpdateDirectory(source, destination string) error {
	return filepath.Walk(source, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := destination
		if relative != "." {
			target = filepath.Join(destination, relative)
		}
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("更新文件不是普通文件：%s", current)
		}
		return copyUpdateFile(current, target, info.Mode().Perm())
	})
}

func replaceUpdateDirectory(source, destination string) error {
	if _, err := os.Stat(source); err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.update-%d", destination, os.Getpid())
	backup := fmt.Sprintf("%s.previous-%d", destination, os.Getpid())
	_ = os.RemoveAll(temporary)
	_ = os.RemoveAll(backup)
	if err := copyUpdateDirectory(source, temporary); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	moved := false
	if _, err := os.Stat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			_ = os.RemoveAll(temporary)
			return err
		}
		moved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.RemoveAll(temporary)
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		if moved {
			_ = os.Rename(backup, destination)
		}
		_ = os.RemoveAll(temporary)
		return err
	}
	if moved {
		_ = os.RemoveAll(backup)
	}
	return nil
}
