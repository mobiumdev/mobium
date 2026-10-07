package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	case "app_clear_data":
		// A phone's reset reinstalls from the bundle, which the daemon reads.
		if path == "" {
			return same, nil
		}
		if err := bundleAsContent(path, args); err != nil {
			return nil, err
		}
		return same, nil
	case "app_install":
		if path == "" {
			return same, nil
		}
		if err := bundleAsContent(path, args); err != nil {
			return nil, err
		}
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

	case "app_audio":
		if action, _ := args["action"].(string); action != "stop" || path == "" {
			return same, nil
		}
		delete(args, "path")
		args["return_data"] = true
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return saveFailedAudio(r, path)
			}
			var v agent.AudioView
			if err := remarshal(r.StructuredContent, &v); err != nil || v.Data == "" {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "the daemon returned no audio to save at %s", path)
			}
			wav, err := base64.StdEncoding.DecodeString(v.Data)
			if err != nil {
				return nil, fmt.Errorf("decode the audio: %w", err)
			}
			if err := writeLocal(path, wav); err != nil {
				return nil, err
			}
			v.Data, v.Path = "", path
			return agent.Result(agent.AudioSavedMessage(path, v), v), nil
		}, nil

	case "app_trace":
		if action, _ := args["action"].(string); action != "stop" || path == "" {
			return same, nil
		}
		delete(args, "path")
		args["return_data"] = true
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return r, nil
			}
			var v agent.TraceView
			if err := remarshal(r.StructuredContent, &v); err != nil || v.Data == "" {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "the daemon returned no trace to save at %s", path)
			}
			raw, err := base64.StdEncoding.DecodeString(v.Data)
			if err != nil {
				return nil, fmt.Errorf("decode the trace: %w", err)
			}
			if err := writeLocal(path, raw); err != nil {
				return nil, err
			}
			v.Data, v.Path = "", path
			return agent.Result(agent.TraceSavedMessage(path, v), v), nil
		}, nil

	case "app_upload":
		if path == "" {
			return same, nil
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "%s is a folder, and a folder cannot be sent to a "+
				"daemon on another machine — only a file can", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no file at %s to upload", path)
		}
		delete(args, "path")
		if _, ok := args["name"]; !ok {
			args["name"] = filepath.Base(path)
		}
		args["content"] = base64.StdEncoding.EncodeToString(raw)
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return r, nil
			}
			var v agent.TransferView
			if err := remarshal(r.StructuredContent, &v); err != nil {
				return r, nil
			}
			v.Path = path
			return agent.Result(fmt.Sprintf("uploaded %s to %s (%d bytes) — confirmed by %s", path, v.Where, v.Bytes, v.Checked), v), nil
		}, nil

	case "app_download":
		if path == "" {
			return same, nil
		}
		delete(args, "path")
		return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
			if r.IsError {
				return r, nil
			}
			var v agent.TransferView
			if err := remarshal(r.StructuredContent, &v); err != nil || v.Data == "" {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "the daemon returned no file to save at %s", path)
			}
			raw, err := base64.StdEncoding.DecodeString(v.Data)
			if err != nil {
				return nil, fmt.Errorf("decode the file: %w", err)
			}
			if int64(len(raw)) != v.Bytes {
				return nil, mobiumerr.New(mobiumerr.NotConfirmed, "the daemon sent %d of %s's %d bytes", len(raw), v.Name, v.Bytes)
			}
			if err := writeLocal(path, raw); err != nil {
				return nil, err
			}
			v.Data, v.Path = "", path
			return agent.Result(agent.DownloadSavedMessage(path, v), v), nil
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

// prepareBatchFiles readies each step of a batch as prepareFiles readies a
// call of its own, and returns what applies each step's finish to that
// step's answer — saving a screenshot where the step asked, say — before the
// batch's answer is rebuilt around them.
func prepareBatchFiles(args map[string]interface{}) (func(*agent.ToolsCallResult) (*agent.ToolsCallResult, error), error) {
	steps, _ := args["steps"].([]interface{})
	finishes := make([]func(*agent.ToolsCallResult) (*agent.ToolsCallResult, error), len(steps))
	for i, item := range steps {
		// A malformed step is left for the daemon, which refuses it with
		// its number before anything runs.
		obj, _ := item.(map[string]interface{})
		name, _ := obj["name"].(string)
		stepArgs, _ := obj["arguments"].(map[string]interface{})
		if name == "" || name == "app_batch" || stepArgs == nil {
			continue
		}
		f, err := prepareFiles(name, stepArgs)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, name, err)
		}
		finishes[i] = f
	}
	return func(r *agent.ToolsCallResult) (*agent.ToolsCallResult, error) {
		if r.IsError {
			return r, nil
		}
		var v agent.BatchView
		if err := remarshal(r.StructuredContent, &v); err != nil {
			return r, nil
		}
		var in, out []agent.Content
		for _, c := range r.Content {
			if c.Type == "image" {
				in = append(in, c)
			}
		}
		for i := range v.Steps {
			st := &v.Steps[i]
			one := &agent.ToolsCallResult{Content: []agent.Content{{Type: "text", Text: st.Text}},
				StructuredContent: st.Data}
			if st.Image && len(in) > 0 {
				one.Content = append(one.Content, in[0])
				in = in[1:]
			}
			if i < len(finishes) && finishes[i] != nil {
				done, err := finishes[i](one)
				if err != nil {
					return nil, fmt.Errorf("step %d (%s): %w", i+1, st.Name, err)
				}
				one = done
			}
			st.Text, st.Data, st.Image = "", one.StructuredContent, false
			var texts []string
			for _, c := range one.Content {
				switch c.Type {
				case "text":
					if c.Text != "" {
						texts = append(texts, c.Text)
					}
				case "image":
					out = append(out, c)
					st.Image = true
				}
			}
			st.Text = strings.Join(texts, "\n")
		}
		return agent.BatchResult(v, out), nil
	}, nil
}

// bundleAsContent replaces an app bundle's path in args with the bundle
// itself: a file as it is, and a .app, which is a directory, as a .tar.gz.
func bundleAsContent(path string, args map[string]interface{}) error {
	info, err := os.Stat(path)
	if err != nil {
		return mobiumerr.New(mobiumerr.InvalidArgument, "no app bundle at %s", path)
	}
	var raw []byte
	name := filepath.Base(path)
	if info.IsDir() {
		raw, err = device.TarGz(path)
		name += ".tar.gz"
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	delete(args, "path")
	args["content"], args["name"] = base64.StdEncoding.EncodeToString(raw), name
	return nil
}

// saveFailedAudio saves the WAV a stop sends back with a failed expectation —
// the evidence of what was heard — where the caller asked, and says so.
func saveFailedAudio(r *agent.ToolsCallResult, path string) (*agent.ToolsCallResult, error) {
	var p mobiumerr.Payload
	if err := remarshal(r.StructuredContent, &p); err != nil {
		return r, nil
	}
	data, _ := p.Details["data"].(string)
	if data == "" {
		return r, nil
	}
	wav, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return r, nil
	}
	if err := writeLocal(path, wav); err != nil {
		return nil, err
	}
	delete(p.Details, "data")
	p.Details["path"] = path
	e := mobiumerr.FromPayload(p)
	e.Message = p.Message + " — the capture is saved at " + path
	res := agent.ErrorResult(e)
	return &res, nil
}
