package app

import (
	"strings"
	"testing"
)

func TestNormalizeGuideInput(t *testing.T) {
	in := guideInput{Title: "  출입 방법  ", Content: "  QR을 제시하세요.  ", Published: true}
	if message := normalizeGuideInput(&in); message != "" {
		t.Fatal(message)
	}
	if in.Title != "출입 방법" || in.Content != "QR을 제시하세요." || in.Category != "일반" {
		t.Fatalf("unexpected normalized guide: %#v", in)
	}

	invalid := []guideInput{
		{Content: "내용"},
		{Title: "제목"},
		{Title: strings.Repeat("가", 201), Content: "내용"},
		{Title: "제목", Category: strings.Repeat("분", 51), Content: "내용"},
	}
	for index := range invalid {
		if message := normalizeGuideInput(&invalid[index]); message == "" {
			t.Fatalf("invalid guide %d was accepted", index)
		}
	}
}
