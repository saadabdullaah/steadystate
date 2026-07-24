Output .artifacts/phase6/acceptance/phase6-admission-denial.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set TypingSpeed 15ms
Set Framerate 2
Set PlaybackSpeed 8.0
Set WaitTimeout 12m

Type "pwsh -NoProfile -File scripts/phase6-acceptance.ps1 -Stage Test"
Enter
Wait+Screen /PHASE6_ACCEPTANCE_RESULT_(PASSED|FAILED)/
Sleep 3s
