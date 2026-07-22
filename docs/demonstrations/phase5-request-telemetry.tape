Output .artifacts/phase5/acceptance/phase5-request-telemetry.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set TypingSpeed 15ms
Set Framerate 2
Set PlaybackSpeed 8.0
Set WaitTimeout 7m

Type "pwsh -NoProfile -File scripts/phase5-acceptance.ps1 -Stage Test"
Enter
Wait+Screen /PHASE5_ACCEPTANCE_RESULT_(PASSED|FAILED)/
Sleep 3s
