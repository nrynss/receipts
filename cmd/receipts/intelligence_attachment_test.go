package main

import (
	"strings"
	"testing"

	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/claims"
	"receipts/internal/intelligence"
	"receipts/internal/judge"
	"receipts/internal/lore"
	"receipts/internal/retrieval"
	"receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch/connection"
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

func TestRuntimeChatRouterWiresProductionServicesAndAuthorityBoundSender(t *testing.T) {
	authority := auth.ChannelContext{AccountID: "owner", ChannelID: "channel", LiveSessionID: "session", Generation: 7}
	capture := &intelligence.Service{Authority: authority}
	ledger := &store.Store{}
	manager := runtime.New(runtime.Config{})
	sender := &connection.Adapter{}
	router := newRuntimeChatRouter(authority, "platform-channel", capture, ledger, manager, sender)
	if router.Authority != authority || router.ChannelPlatformID != "platform-channel" {
		t.Fatalf("router authority=%+v platform=%q", router.Authority, router.ChannelPlatformID)
	}
	if got, ok := router.Capture.(*intelligence.Service); !ok || got != capture {
		t.Fatalf("capture=%T", router.Capture)
	}
	if got, ok := router.Appeals.(appeal.Service); !ok || got.Store != ledger || got.Runtime != manager {
		t.Fatalf("appeals=%#v", router.Appeals)
	}
	if got, ok := router.Lore.(lore.Service); !ok || got.Ledger != ledger {
		t.Fatalf("lore=%#v", router.Lore)
	}
	if got, ok := router.Sender.(*connection.Adapter); !ok || got != sender {
		t.Fatalf("sender=%T", router.Sender)
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
