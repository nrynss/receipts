package main

import (
	"strings"
	"testing"

	"receipts/internal/auth"
	"receipts/internal/claims"
	"receipts/internal/judge"
	"receipts/internal/retrieval"
)

func TestIntelligenceAttachmentBuildsLunaAndDecisionsAdapters(t *testing.T) {
	attachment := &intelligenceAttachment{apiKey: "offline-test-key", embeddingVersion: "claim-v1", embeddingDimensions: 1536}
	svc, err := attachment.buildIntelligenceService(auth.ChannelContext{})
	if err != nil {
		t.Fatal(err)
	}
	extractor, ok := svc.Extractor.(claims.Client)
	if !ok || extractor.Model != claims.LunaModel || svc.ExtractionModel != claims.LunaModel {
		t.Fatalf("extractor=%T model=%q extraction model=%q", svc.Extractor, extractor.Model, svc.ExtractionModel)
	}
	if _, ok := svc.Sensitive.(claims.DecisionsClient); !ok {
		t.Fatalf("sensitivity adapter=%T", svc.Sensitive)
	}
	if _, ok := svc.Judge.(judge.DecisionsClient); !ok {
		t.Fatalf("judge adapter=%T", svc.Judge)
	}
	if svc.Space.Model != retrieval.DefaultModel {
		t.Fatalf("embedding model changed to %q", svc.Space.Model)
	}
}

func TestIntelligenceAttachmentRejectsLegacyProductionModels(t *testing.T) {
	base := intelligenceAttachment{apiKey: "offline-test-key", embeddingVersion: "claim-v1", embeddingDimensions: 1536}
	base.extractionModel = claims.LegacyModel
	if _, err := base.buildIntelligenceService(auth.ChannelContext{}); err == nil || !strings.Contains(err.Error(), "RECEIPTS_EXTRACTION_MODEL") {
		t.Fatalf("legacy extraction model error=%v", err)
	}
	base.extractionModel = claims.LunaModel
	base.judgeModel = "gpt-4o-mini-2024-07-18"
	if _, err := base.buildIntelligenceService(auth.ChannelContext{}); err == nil || !strings.Contains(err.Error(), "RECEIPTS_JUDGE_MODEL") {
		t.Fatalf("legacy judge model error=%v", err)
	}
}
