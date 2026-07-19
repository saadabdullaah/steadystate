Output .artifacts/phase5/acceptance/phase5-request-telemetry.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set TypingSpeed 15ms
Set PlaybackSpeed 1.5
Set WaitTimeout 20m

Type "pwsh -NoProfile -File scripts/phase5-acceptance.ps1 -Stage Test"
Enter
Wait+Screen /PHASE5_ACCEPTANCE_RESULT_(PASSED|FAILED)/
Sleep 3s
