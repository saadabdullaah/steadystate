Output .artifacts/phase7/acceptance/phase7-disaster-recovery.gif
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set TypingSpeed 15ms
Set Framerate 2
Set PlaybackSpeed 12.0
Set WaitTimeout 65m

Type "pwsh -NoProfile -File scripts/phase7-acceptance.ps1 -Stage Test || (echo PHASE7_ACCEPTANCE_RESULT_FAILED; exit 1)"
Enter
Wait+Screen /PHASE7_ACCEPTANCE_RESULT_(PASSED|FAILED)/
Sleep 3s
