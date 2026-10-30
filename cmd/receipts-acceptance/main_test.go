//go:build acceptance

package main

import "testing"

func TestValidPublicClip(t *testing.T) {
	const id = "FairMagnificentTofuAsianGlow-AuW2QKA-Y7u4bdL_"
	for _, rawURL := range []string{
		"https://clips.twitch.tv/" + id,
		"https://www.twitch.tv/nrynss/clip/" + id,
	} {
		if !validPublicClip(id, rawURL) {
			t.Errorf("validPublicClip(%q) = false", rawURL)
		}
	}
	for _, rawURL := range []string{
		"http://clips.twitch.tv/" + id,
		"https://clips.twitch.tv.evil.test/" + id,
		"https://user@clips.twitch.tv/" + id,
		"https://clips.twitch.tv/" + id + "?autoplay=true",
		"https://www.twitch.tv/nrynss/clips/" + id,
		"https://www.twitch.tv/not-a-login/clip/" + id,
	} {
		if validPublicClip(id, rawURL) {
			t.Errorf("validPublicClip(%q) = true", rawURL)
		}
	}
	if validPublicClip(id, "https://clips.twitch.tv/other") {
		t.Fatal("mismatched ID accepted")
	}
	if validPublicClip("../other", "https://clips.twitch.tv/other") {
		t.Fatal("invalid clip ID accepted")
	}
}
