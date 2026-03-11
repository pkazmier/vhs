Output examples/settings/set-caption-with-audio-overlay-title.mp4
Set Width 800
Set Height 225
Set Padding 50

Set FontSize 18
Set TypingSpeed 0.1
Set Theme "Catppuccin Macchiato"

Set CaptionAudio "thock"
Set CaptionAudioVolume 0.15

Set OverlayFontColor "#c6a0f6"
Set OverlayBoxOpacity 0

CaptionOn

Overlay@4s "Keypress Audio Demo"
Type "You should hear audio as this is captioned."
Sleep 2s
Ctrl+u

Overlay@4s "Audio Overlay Demo"
Set AudioVolume 1.0
Audio examples/settings/audio-overlay-demo.m4a
Type "You can play arbitrary audio files in the video."
Sleep 2s

