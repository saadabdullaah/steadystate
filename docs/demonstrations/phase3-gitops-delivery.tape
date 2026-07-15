Output docs/demonstrations/phase3-gitops-delivery.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 20
Set Margin 24
Set MarginFill "#111827"
Set Theme "Catppuccin Mocha"
Set TypingSpeed 5ms
Set WaitTimeout 25m

Type "pwsh -NoProfile -File scripts/phase3-acceptance.ps1 -Profile standard -EvidencePath .artifacts/phase3/acceptance.json"
Enter
Wait+Screen /SteadyState Phase 3 GitOps delivery acceptance passed\./
Sleep 3s
