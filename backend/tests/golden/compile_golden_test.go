package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keepbuild/seewxapkg/internal/pipeline/classifier"
	"github.com/keepbuild/seewxapkg/internal/pipeline/normalize"
	"github.com/keepbuild/seewxapkg/internal/service"
	"github.com/keepbuild/seewxapkg/tests/testutil"
)

func TestGoldenProfilesAndManifest(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "standard-unencrypted",
			files: map[string]string{
				"app.json":              `{"pages":["pages/home/index"]}`,
				"app.js":                `App({})`,
				"pages/home/index.js":   `Page({})`,
				"pages/home/index.wxml": `<view>home</view>`,
				"pages/home/index.wxss": `.home {}`,
			},
		},
		{
			name: "wechat4x-main",
			files: map[string]string{
				"app-config.json": `{"pages":["pages/home/index"],"global":{"window":{"navigationBarTitleText":"Demo"}}}`,
				"app-service.js":  `var __wxAppCode__ = {};`,
				"page-frame.html": `<template name="p"><view>frame</view></template>`,
				"app-wxss.js":     `setCssToHead([],undefined,{path:"app.wxss"});`,
			},
		},
		{
			name: "page-frame-html",
			files: map[string]string{
				"app-config.json": `{"pages":["pages/frame/index"]}`,
				"page-frame.html": `<template name="f"><view>frame</view></template>`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := testutil.MustBuildWxapkg(tc.files)
			extractedDir := t.TempDir()
			if _, err := service.UnpackWxapkg(data, extractedDir, false); err != nil {
				t.Fatalf("UnpackWxapkg: %v", err)
			}

			profile, err := classifier.DetectPackageProfile(data, extractedDir)
			if err != nil {
				t.Fatalf("DetectPackageProfile: %v", err)
			}
			normalized, err := normalize.NormalizePackage(extractedDir, profile)
			if err != nil {
				t.Fatalf("NormalizePackage: %v", err)
			}

			assertGoldenSubset(t, filepath.Join("..", "fixtures", tc.name, "expected", "profile.json"), profile)
			assertGoldenSubset(t, filepath.Join("..", "fixtures", tc.name, "expected", "manifest.json"), normalized.Manifest)
		})
	}
}

func assertGoldenSubset(t *testing.T, fixturePath string, actual interface{}) {
	t.Helper()

	expectedData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}

	var expected map[string]interface{}
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("decode fixture %s: %v", fixturePath, err)
	}

	actualData, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("encode actual: %v", err)
	}
	var actualMap map[string]interface{}
	if err := json.Unmarshal(actualData, &actualMap); err != nil {
		t.Fatalf("decode actual json: %v", err)
	}

	if err := assertSubset(expected, actualMap); err != nil {
		t.Fatalf("fixture %s mismatch: %v\nactual=%s", fixturePath, err, string(actualData))
	}
}

func assertSubset(expected, actual map[string]interface{}) error {
	for key, value := range expected {
		actualValue, ok := actual[key]
		if !ok {
			return &subsetError{message: "missing key: " + key}
		}
		switch expectedTyped := value.(type) {
		case map[string]interface{}:
			actualTyped, ok := actualValue.(map[string]interface{})
			if !ok {
				return &subsetError{message: "type mismatch at key: " + key}
			}
			if err := assertSubset(expectedTyped, actualTyped); err != nil {
				return err
			}
		default:
			expectedBytes, _ := json.Marshal(expectedTyped)
			actualBytes, _ := json.Marshal(actualValue)
			if string(expectedBytes) != string(actualBytes) {
				return &subsetError{message: "value mismatch at key: " + key}
			}
		}
	}
	return nil
}

type subsetError struct {
	message string
}

func (e *subsetError) Error() string {
	return e.message
}
