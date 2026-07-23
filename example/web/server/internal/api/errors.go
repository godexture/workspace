package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/godexture/sdk/conversion"
	"github.com/labstack/echo/v4"
)

// writeError maps err to the shared {code, message} envelope and an HTTP
// status derived from its conversion.Code, so the Server and WASM frontends
// can branch on the same codes regardless of transport.
func writeError(c echo.Context, err error) error {
	convErr := toConversionError(err)
	return writeJSON(c, statusForCode(convErr.Code), map[string]*conversion.Error{"error": convErr})
}

func toConversionError(err error) *conversion.Error {
	var convErr *conversion.Error
	if errors.As(err, &convErr) {
		return convErr
	}
	return &conversion.Error{Code: conversion.CodeInternal, Message: err.Error()}
}

func statusForCode(code conversion.Code) int {
	switch code {
	case conversion.CodeInvalidSpec, conversion.CodeUnsupportedCodec:
		return http.StatusBadRequest
	case conversion.CodeNotFound:
		return http.StatusNotFound
	case conversion.CodeNotReady, conversion.CodeCanceled:
		return http.StatusConflict
	case conversion.CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case conversion.CodeNegotiationFailed, conversion.CodeBuildFailed, conversion.CodePipelineFailed:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// writeJSON marshals value before writing the status line, so a marshal
// failure still produces a proper error response instead of a committed
// 200 with an empty/truncated body (echo's own c.JSON has this same
// write-then-encode footgun, which is why this doesn't just call it).
func writeJSON(c echo.Context, status int, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("api: failed to encode JSON response: %v", err)
		return c.String(http.StatusInternalServerError, `{"error":{"code":"internal","message":"failed to encode response"}}`)
	}
	return c.Blob(status, echo.MIMEApplicationJSON, data)
}
