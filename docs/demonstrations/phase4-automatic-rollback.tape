Output .artifacts/phase4/acceptance/phase4-automatic-rollback.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 20
Set Margin 24
Set MarginFill "#111827"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 5ms
Set WaitTimeout 28m
Set Framerate 2
Set PlaybackSpeed 8.0

Type "if timeout --signal=TERM --kill-after=10s 27m pwsh -NoProfile -File scripts/phase4-acceptance.ps1 -Stage Rollback -Profile standard -EvidencePath .artifacts/phase4/acceptance/evidence.json; then echo PHASE4_ROLLBACK_RESULT_PASSED; else echo PHASE4_ROLLBACK_RESULT_FAILED; fi; sleep 10"
Enter
Wait /PHASE4_ROLLBACK_RESULT_(PASSED|FAILED)/
Sleep 3s
