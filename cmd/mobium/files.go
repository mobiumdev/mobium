package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// filesAsContent says the daemon's disk is not the caller's, so file
// arguments travel as content. MOBIUM_FILES=content sets it; a daemon reached
// over a remote transport will set it too.
func filesAsContent() bool { return os.Getenv("MOBIUM_FILES") == "content" }

// sendFilesAsContent rewrites a call's file arguments — agent.PathArguments,
// already absolute — as the files themselves, and returns what to do with the
// result: save a screenshot or a recording where the caller asked, and answer
// as the daemon would have had it saved them itself. Found by running a
// client against a daemon over SSH, where every path named the daemon's disk.
func sendFilesAsContent(tool string, args map[string]interface{}) (func(*agent.ToolsCallResult) (*agent.ToolsCallResult, error), error) {
	same := func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) { return r, nil }
	path, _ := args["path"].(string)
	switch tool {
	case "app_install":
		if path == "" {
			return same, nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no app bundle at %s", path)
		}
		var raw []byte
		name := filepath.Base(path)
		if info.IsDir() {
			// A .app is a directory, and goes as an archive of it.
			raw, err = device.TarGz(path)
			name += ".tar.gz"
		} else {
			raw, err = os.ReadFile(path)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		delete(args, "path")
		args["content"], args["name"] = base64.StdEncoding.EncodeToString(raw), name
		// The daemon installed a copy in a directory of its own, and names
		// that; the caller named this one.
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return r, nil
			}
			var v agent.InstallView
			if err := remarshal(r.StructuredContent, &v); err != nil {
				return r, nil
			}
			v.Path = path
			return agent.Result("installed "+path, v), nil
		}, nil

	case "app_location":
		gpx, _ := args["gpx"].(string)
		if gpx == "" {
			return same, nil
		}
		raw, err := os.ReadFile(gpx)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", gpx, err)
		}
		delete(args, "gpx")
		args["gpx_data"] = string(raw)
		return same, nil

	case "app_record":
		if action, _ := args["action"].(string); action != "stop" || path == "" {
			return same, nil
		}
		delete(args, "path")
		args["return_data"] = true
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return r, nil
			}
			var v agent.RecordView
			if err := remarshal(r.StructuredContent, &v); err != nil || v.Data == "" {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "the daemon returned no video to save at %s", path)
			}
			video, err := base64.StdEncoding.DecodeString(v.Data)
			if err != nil {
				return nil, fmt.Errorf("decode the video: %w", err)
			}
			if err := writeLocal(path, video); err != nil {
				return nil, err
			}
			v.Data, v.Path = "", path
			return agent.Result(agent.RecordSavedMessage(path, v), v), nil
		}, nil

	case "app_screenshot":
		if path == "" {
			return same, nil
		}
		delete(args, "path")
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return r, nil
			}
			for _, c := range r.Content {
				if c.Type != "image" {
					continue
				}
				png, err := base64.StdEncoding.DecodeString(c.Data)
				if err != nil {
					return nil, fmt.Errorf("decode the screenshot: %w", err)
				}
				if err := writeLocal(path, png); err != nil {
					return nil, err
				}
				return agent.Result(agent.ScreenshotSavedMessage(path, len(png)),
					agent.ScreenshotView{Path: path, Bytes: len(png)}), nil
			}
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "the daemon returned no image to save at %s", path)
		}, nil
	}
	return same, nil
}

// writeLocal saves a file the daemon sent back, on the caller's own disk.
func writeLocal(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// remarshal turns a result's structured content, decoded as generic JSON on
// its way through the socket, back into the view it was made from.
func remarshal(from interface{}, to interface{}) error {
	raw, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, to)
}
