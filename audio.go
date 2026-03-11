package main

import (
	_ "embed"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AudioEvent represents a single audio clip placed at a specific point in the recording.
type AudioEvent struct {
	StartMs    int64
	DurationMs int64 // 0 = play full duration
	Volume     float64
	FilePath   string
}

// AudioOptions holds mid-tape changeable audio settings.
type AudioOptions struct {
	Volume float64 // default 1.0, range 0.0-1.0
}

//go:embed sounds/click.wav
var clickSound []byte

//go:embed sounds/clack.wav
var clackSound []byte

//go:embed sounds/thock.wav
var thockSound []byte

var presetSounds = map[string][]byte{
	"click": clickSound,
	"clack": clackSound,
	"thock": thockSound,
}

// resolveCaptionAudioFile returns the file path for a caption audio preset or
// user-supplied file. If preset, writes the embedded WAV to tempDir.
func resolveCaptionAudioFile(preset, tempDir string) (string, error) {
	if data, ok := presetSounds[preset]; ok {
		outPath := filepath.Join(tempDir, "caption-sound-"+preset+".wav")
		if err := os.WriteFile(outPath, data, 0o600); err != nil {
			return "", fmt.Errorf("failed to write preset sound %s: %w", preset, err)
		}
		return outPath, nil
	}
	// Treat as file path
	if _, err := os.Stat(preset); err != nil {
		return "", fmt.Errorf("audio file not found: %s", preset)
	}
	return preset, nil
}

const audioBatchSize = 100

// GenerateAudioTrack mixes caption click sounds and Audio command clips into
// a single WAV file. Returns the path to the mixed file, or "" if no audio.
func GenerateAudioTrack(
	keyEvents []KeyEvent,
	audioEvents []AudioEvent,
	captionOpts CaptionOptions,
	videoOpts VideoOptions,
) (string, error) {
	tempDir := videoOpts.Input
	playbackSpeed := videoOpts.PlaybackSpeed

	var captionSoundFile string
	if captionOpts.Audio != "" && len(keyEvents) > 0 {
		var err error
		captionSoundFile, err = resolveCaptionAudioFile(captionOpts.Audio, tempDir)
		if err != nil {
			return "", err
		}
	}

	hasCaptionAudio := captionSoundFile != ""
	hasAudioEvents := len(audioEvents) > 0

	if !hasCaptionAudio && !hasAudioEvents {
		return "", nil
	}

	// Calculate total duration from the last frame
	totalFrames := 0
	entries, _ := os.ReadDir(videoOpts.Input)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "frame-text-") {
			totalFrames++
		}
	}
	if totalFrames == 0 {
		totalFrames = 1
	}
	totalDurationMs := int64(totalFrames) * 1000 / int64(videoOpts.Framerate)

	// Adjust for playback speed
	if playbackSpeed != 0 && playbackSpeed != 1.0 {
		totalDurationMs = int64(float64(totalDurationMs) / playbackSpeed)
	}
	totalDurationSec := float64(totalDurationMs) / 1000.0

	outputPath := filepath.Join(tempDir, "audio-mix.wav")

	if hasCaptionAudio && len(keyEvents) > audioBatchSize {
		return generateAudioBatched(keyEvents, audioEvents, captionSoundFile, captionOpts, playbackSpeed, totalDurationSec, tempDir, outputPath)
	}

	return generateAudioSingle(keyEvents, audioEvents, captionSoundFile, captionOpts, playbackSpeed, totalDurationSec, outputPath)
}

func generateAudioSingle(
	keyEvents []KeyEvent,
	audioEvents []AudioEvent,
	captionSoundFile string,
	captionOpts CaptionOptions,
	playbackSpeed float64,
	totalDurationSec float64,
	outputPath string,
) (string, error) {
	var args []string
	inputIdx := 0

	// Input 0: silence base track
	args = append(args, "-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("anullsrc=r=44100:cl=mono:d=%f", totalDurationSec),
	)
	silenceIdx := inputIdx
	inputIdx++

	// Add caption click sound as input
	captionInputIdx := -1
	if captionSoundFile != "" && len(keyEvents) > 0 {
		args = append(args, "-i", captionSoundFile)
		captionInputIdx = inputIdx
		inputIdx++
	}

	// Add audio event files as inputs
	audioInputIdxs := make([]int, len(audioEvents))
	for i, ae := range audioEvents {
		args = append(args, "-i", ae.FilePath)
		audioInputIdxs[i] = inputIdx
		inputIdx++
	}

	// Build filter_complex
	var filters []string
	var mixInputs []string
	mixInputs = append(mixInputs, fmt.Sprintf("[%d:a]", silenceIdx))
	streamIdx := 0

	// Caption clicks
	if captionInputIdx >= 0 {
		volume := captionOpts.AudioVolume
		for _, ke := range keyEvents {
			delayMs := ke.StartMs
			if playbackSpeed != 0 && playbackSpeed != 1.0 {
				delayMs = int64(float64(delayMs) / playbackSpeed)
			}
			outLabel := fmt.Sprintf("ck%d", streamIdx)
			filters = append(filters,
				fmt.Sprintf("[%d:a]adelay=%d|%d,volume=%f[%s]",
					captionInputIdx, delayMs, delayMs, volume, outLabel))
			mixInputs = append(mixInputs, fmt.Sprintf("[%s]", outLabel))
			streamIdx++
		}
	}

	// Audio events
	for i, ae := range audioEvents {
		delayMs := ae.StartMs
		if playbackSpeed != 0 && playbackSpeed != 1.0 {
			delayMs = int64(float64(delayMs) / playbackSpeed)
		}
		outLabel := fmt.Sprintf("au%d", i)
		trimFilter := ""
		if ae.DurationMs > 0 {
			durationMs := ae.DurationMs
			if playbackSpeed != 0 && playbackSpeed != 1.0 {
				durationMs = int64(float64(durationMs) / playbackSpeed)
			}
			durationSec := float64(durationMs) / 1000.0
			trimFilter = fmt.Sprintf("atrim=duration=%f,asetpts=PTS-STARTPTS,", durationSec)
		}
		filters = append(filters,
			fmt.Sprintf("[%d:a]%sadelay=%d|%d,volume=%f[%s]",
				audioInputIdxs[i], trimFilter, delayMs, delayMs, ae.Volume, outLabel))
		mixInputs = append(mixInputs, fmt.Sprintf("[%s]", outLabel))
	}

	// Mix all streams
	nInputs := len(mixInputs)
	if nInputs > 1 {
		mixFilter := fmt.Sprintf("%samix=inputs=%d:duration=longest:normalize=0[out]",
			strings.Join(mixInputs, ""), nInputs)
		filters = append(filters, mixFilter)
	} else {
		// Only silence, rename
		filters = append(filters, fmt.Sprintf("[%d:a]acopy[out]", silenceIdx))
	}

	filterComplex := strings.Join(filters, ";")
	args = append(args, "-filter_complex", filterComplex, "-map", "[out]", "-ac", "1", "-ar", "44100", outputPath)

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to generate audio track: %w\n%s", err, string(out))
	}

	return outputPath, nil
}

func generateAudioBatched(
	keyEvents []KeyEvent,
	audioEvents []AudioEvent,
	captionSoundFile string,
	captionOpts CaptionOptions,
	playbackSpeed float64,
	totalDurationSec float64,
	tempDir string,
	outputPath string,
) (string, error) {
	// Split key events into batches and generate intermediate WAVs
	nBatches := int(math.Ceil(float64(len(keyEvents)) / float64(audioBatchSize)))
	intermediateFiles := make([]string, 0, nBatches)

	for batch := range nBatches {
		start := batch * audioBatchSize
		end := start + audioBatchSize
		if end > len(keyEvents) {
			end = len(keyEvents)
		}
		batchEvents := keyEvents[start:end]

		batchOutput := filepath.Join(tempDir, fmt.Sprintf("audio-batch-%03d.wav", batch))

		// For batches, pass no audioEvents (they go in final mix)
		var batchAudioEvents []AudioEvent
		_, err := generateAudioSingle(batchEvents, batchAudioEvents, captionSoundFile, captionOpts, playbackSpeed, totalDurationSec, batchOutput)
		if err != nil {
			return "", fmt.Errorf("failed to generate audio batch %d: %w", batch, err)
		}
		intermediateFiles = append(intermediateFiles, batchOutput)
	}

	// Final mix: merge intermediate files + audio events
	var args []string
	inputIdx := 0

	args = append(args, "-y")

	for _, f := range intermediateFiles {
		args = append(args, "-i", f)
		inputIdx++
	}

	// Add audio event files
	audioInputIdxs := make([]int, len(audioEvents))
	for i, ae := range audioEvents {
		args = append(args, "-i", ae.FilePath)
		audioInputIdxs[i] = inputIdx
		inputIdx++
	}

	var filters []string
	var mixInputs []string

	for i := range intermediateFiles {
		mixInputs = append(mixInputs, fmt.Sprintf("[%d:a]", i))
	}

	for i, ae := range audioEvents {
		delayMs := ae.StartMs
		if playbackSpeed != 0 && playbackSpeed != 1.0 {
			delayMs = int64(float64(delayMs) / playbackSpeed)
		}
		outLabel := fmt.Sprintf("au%d", i)
		trimFilter := ""
		if ae.DurationMs > 0 {
			durationMs := ae.DurationMs
			if playbackSpeed != 0 && playbackSpeed != 1.0 {
				durationMs = int64(float64(durationMs) / playbackSpeed)
			}
			durationSec := float64(durationMs) / 1000.0
			trimFilter = fmt.Sprintf("atrim=duration=%f,asetpts=PTS-STARTPTS,", durationSec)
		}
		filters = append(filters,
			fmt.Sprintf("[%d:a]%sadelay=%d|%d,volume=%f[%s]",
				audioInputIdxs[i], trimFilter, delayMs, delayMs, ae.Volume, outLabel))
		mixInputs = append(mixInputs, fmt.Sprintf("[%s]", outLabel))
	}

	nInputs := len(mixInputs)
	mixFilter := fmt.Sprintf("%samix=inputs=%d:duration=longest:normalize=0[out]",
		strings.Join(mixInputs, ""), nInputs)
	filters = append(filters, mixFilter)

	filterComplex := strings.Join(filters, ";")
	args = append(args, "-filter_complex", filterComplex, "-map", "[out]", "-ac", "1", "-ar", "44100", outputPath)

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to generate final audio mix: %w\n%s", err, string(out))
	}

	// Clean up intermediate files
	for _, f := range intermediateFiles {
		_ = os.Remove(f)
	}

	return outputPath, nil
}
