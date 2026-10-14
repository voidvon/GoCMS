package captcha

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wenlng/go-captcha-assets/resources/imagesv2"
	"github.com/wenlng/go-captcha-assets/resources/tiles"
	"github.com/wenlng/go-captcha/v2/slide"
)

// SlideData represents the payload returned to the client to render the slider puzzle.
type SlideData struct {
	Key         string `json:"key"`
	ImageBase64 string `json:"image"`
	TileBase64  string `json:"tile"`
	TileWidth   int    `json:"tile_width"`
	TileHeight  int    `json:"tile_height"`
	TileX       int    `json:"tile_x"`
	TileY       int    `json:"tile_y"`
}

type cacheItem struct {
	block     *slide.Block
	createdAt time.Time
	verified  bool
}

// Service manages slide captcha generation, coordinate validation, and token consumption.
type Service struct {
	captcha   slide.Captcha
	mu        sync.RWMutex
	cache     map[string]*cacheItem
	expireDur time.Duration
	stopCh    chan struct{}
}

// NewService initializes a slide captcha service with embedded assets.
func NewService() (*Service, error) {
	builder := slide.NewBuilder()

	imgs, err := imagesv2.GetImages()
	if err != nil {
		return nil, fmt.Errorf("load captcha background images: %w", err)
	}

	graphs, err := tiles.GetTiles()
	if err != nil {
		return nil, fmt.Errorf("load captcha tile graphs: %w", err)
	}

	newGraphs := make([]*slide.GraphImage, 0, len(graphs))
	for _, graph := range graphs {
		newGraphs = append(newGraphs, &slide.GraphImage{
			OverlayImage: graph.OverlayImage,
			MaskImage:    graph.MaskImage,
			ShadowImage:  graph.ShadowImage,
		})
	}

	builder.SetResources(
		slide.WithGraphImages(newGraphs),
		slide.WithBackgrounds(imgs),
	)

	svc := &Service{
		captcha:   builder.Make(),
		cache:     make(map[string]*cacheItem),
		expireDur: 5 * time.Minute,
		stopCh:    make(chan struct{}),
	}

	go svc.cleanupLoop()

	return svc, nil
}

// Close stops background cleanup.
func (s *Service) Close() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *Service) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanExpired()
		}
	}
}

func (s *Service) cleanExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, item := range s.cache {
		if now.Sub(item.createdAt) > s.expireDur {
			delete(s.cache, k)
		}
	}
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateSlide generates a new slider puzzle challenge and records its block coordinates.
func (s *Service) GenerateSlide() (*SlideData, error) {
	captData, err := s.captcha.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate captcha: %w", err)
	}

	blockData := captData.GetData()
	if blockData == nil {
		return nil, errors.New("empty captcha block data")
	}

	masterBase64, err := captData.GetMasterImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("master image base64: %w", err)
	}

	tileBase64, err := captData.GetTileImage().ToBase64()
	if err != nil {
		return nil, fmt.Errorf("tile image base64: %w", err)
	}

	key := "capt_" + randomHex(16)

	s.mu.Lock()
	s.cache[key] = &cacheItem{
		block:     blockData,
		createdAt: time.Now(),
		verified:  false,
	}
	s.mu.Unlock()

	return &SlideData{
		Key:         key,
		ImageBase64: masterBase64,
		TileBase64:  tileBase64,
		TileWidth:   blockData.Width,
		TileHeight:  blockData.Height,
		TileX:       blockData.DX,
		TileY:       blockData.DY,
	}, nil
}

// ValidateSlide validates user dragging coordinates against the expected position.
// Tolerance is set to 6 pixels.
func (s *Service) ValidateSlide(key string, userX, userY int) bool {
	if key == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists := s.cache[key]
	if !exists {
		return false
	}

	if time.Since(item.createdAt) > s.expireDur {
		delete(s.cache, key)
		return false
	}

	// Tolerance of 6px
	if !slide.Validate(userX, userY, item.block.X, item.block.Y, 6) {
		return false
	}

	item.verified = true
	item.createdAt = time.Now() // reset timestamp so verified token has 5 minutes lifetime
	return true
}

// VerifyAndConsume checks if the token was verified and consumes it immediately.
func (s *Service) VerifyAndConsume(token string) bool {
	if token == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, exists := s.cache[token]
	if !exists {
		return false
	}

	delete(s.cache, token)

	if time.Since(item.createdAt) > s.expireDur {
		return false
	}

	return item.verified
}

// GetTargetCoordinatesForTest returns the target coordinates for testing purposes.
func (s *Service) GetTargetCoordinatesForTest(key string) (int, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, exists := s.cache[key]
	if !exists || item.block == nil {
		return 0, 0, false
	}
	return item.block.X, item.block.Y, true
}
