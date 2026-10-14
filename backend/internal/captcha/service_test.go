package captcha

import (
	"testing"
)

func TestSlideCaptchaFlow(t *testing.T) {
	svc, err := NewService()
	if err != nil {
		t.Fatalf("failed to create captcha service: %v", err)
	}
	defer svc.Close()

	slideData, err := svc.GenerateSlide()
	if err != nil {
		t.Fatalf("failed to generate slide: %v", err)
	}

	if slideData.Key == "" {
		t.Fatalf("expected non-empty key")
	}
	if slideData.ImageBase64 == "" || slideData.TileBase64 == "" {
		t.Fatalf("expected non-empty base64 images")
	}

	// 1. Wrong coordinates should fail
	if svc.ValidateSlide(slideData.Key, 0, 0) {
		t.Fatalf("expected validation failure for wrong coordinates")
	}

	// Retrieve actual coordinates from internal cache
	svc.mu.RLock()
	item := svc.cache[slideData.Key]
	targetX := item.block.X
	targetY := item.block.Y
	svc.mu.RUnlock()

	// 2. Correct coordinates within tolerance should pass
	if !svc.ValidateSlide(slideData.Key, targetX+2, targetY) {
		t.Fatalf("expected validation success for coordinates within tolerance")
	}

	// 3. VerifyAndConsume should succeed once
	if !svc.VerifyAndConsume(slideData.Key) {
		t.Fatalf("expected successful token consumption")
	}

	// 4. Repeated consumption should fail (replay prevention)
	if svc.VerifyAndConsume(slideData.Key) {
		t.Fatalf("expected token to be consumed and fail on replay")
	}
}
