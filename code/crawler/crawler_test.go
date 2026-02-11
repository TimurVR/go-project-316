package code_test

import (
	"context"
	"encoding/json"
	code "hexlet-go-crawler/code/crawler"
	"testing"
)

func TestSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url := "https://example.com"
	test := code.Options{URL: url}
	res, err := code.Analyze(ctx, test)
	if err != nil {
		t.Errorf("Ошибка в функции %s", err)
	}
	testreport := &code.Report{}
	err = json.Unmarshal(res, testreport)
	if err != nil {
		t.Errorf("Ошибка в unmarshal %s", err)
	}
	if testreport.Pages[0].Status != "ok" {
		t.Error("Непрвильный статус")
	}
}
func TestAnalyze_InvalidURL(t *testing.T) {
	opts := code.Options{
		URL: "",
	}

	ctx := context.Background()
	_, err := code.Analyze(ctx, opts)
	if err == nil {
		t.Error("Ожидалась ошибка при пустом URL")
	}
}

func TestAnalyze_NetworkError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url := "http://localhost:99999/invalid"
	test := code.Options{URL: url}
	res, err := code.Analyze(ctx, test)
	if err != nil {
		t.Errorf("Ошибка в функции %s", err)
	}
	testreport := &code.Report{}
	err = json.Unmarshal(res, testreport)
	if err != nil {
		t.Errorf("Ошибка в unmarshal %s", err)
	}
	if testreport.Pages[0].Error == "" {
		t.Error("Отсутствие ошибки")
	}
	if testreport.Pages[0].Status == "ok" {
		t.Error("Непрвильный статус")
	}
}
