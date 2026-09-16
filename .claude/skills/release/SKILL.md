---
name: release
description: Cut and publish a release of this repo, and optionally install it locally. Use when the user says "release a patch", "cut a release", "release minor", "ship it", or asks to publish a new version and update their local install.
---

# Release

Publishes a new version of Windows-StayWakeBlackScreen: verify, commit, tag,
let CI build the exes and publish the GitHub Release, then (optionally)
install that release on this machine and confirm it took.

**Argument:** `patch` (default), `minor`, or `major` — which part of the
version to bump. "and install" anywhere in the request also runs step 6.

## Steps

1. **Check the working tree.** `git status --short`. If there is nothing to
   commit and `HEAD` is already tagged, stop and say so — there is nothing
   to release.

2. **Verify before releasing.** Go lives at `/usr/local/go/bin` on this
   machine, so put it on PATH first:
   ```sh
   export PATH=$PATH:/usr/local/go/bin
   gofmt -l .                                        # must print nothing
   go test ./...                                     # OS-independent packages
   GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...
   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
   ```
   Any failure stops the release — report it instead of pushing.

3. **Commit** anything uncommitted, with a message describing the change and
   its reason (not a file list). Do not commit files containing secrets.

4. **Tag and push.** Next version = highest existing tag
   (`git tag --sort=-v:refname | head -1`) with the requested part bumped.
   Push the branch first, then the annotated tag, whose message lists the
   user-visible changes:
   ```sh
   git push origin main
   git tag -a vX.Y.Z -m "vX.Y.Z - <summary>

   - <user-visible change>"
   git push origin vX.Y.Z
   ```

5. **Watch CI.** The tag push triggers `.github/workflows/build.yml`, which
   tests, builds all three exes and publishes the Release; the branch push
   triggers the `ci.yml` check. Watch both and report failures with the
   failing step:
   ```sh
   id=$(gh run list --workflow build.yml --branch vX.Y.Z --limit 1 --json databaseId -q '.[0].databaseId')
   gh run watch "$id" --exit-status
   gh release view vX.Y.Z --json assets -q '.assets[].name'
   ```

6. **Install locally** (only if asked). This machine is WSL, so Windows
   programs run through `/mnt/c`. Download the released Setup into the
   Windows temp folder and run it non-interactively:
   ```sh
   T=/mnt/c/Users/$USER/AppData/Local/Temp
   gh release download vX.Y.Z -p Setup_StayWakeBlackScreenIdle.exe -D "$T" --clobber
   (cd "$T" && ./Setup_StayWakeBlackScreenIdle.exe -mode install)
   rm -f "$T/Setup_StayWakeBlackScreenIdle.exe"
   ```
   Setup stops the running copy, replaces it, registers autostart and starts
   the new one.

7. **Verify the install** (only after step 6) and report each result:
   - version: `grep -aoE 'v[0-9]+\.[0-9]+\.[0-9]+' "/mnt/c/Users/$USER/AppData/Local/StayWakeBlackScreen/StayWakeBlackScreenIdle.exe" | sort -u`
   - exactly one copy running: `tasklist.exe /FI "IMAGENAME eq StayWakeBlackScreenIdle.exe"`
   - autostart: `reg.exe query 'HKCU\Software\Microsoft\Windows\CurrentVersion\Run' /v StayWakeBlackScreenIdle`
   - tray icon (its tooltip carries the version):
     ```sh
     powershell.exe -NoProfile -Command 'Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes; $A=[System.Windows.Automation.AutomationElement]; @($A::RootElement.FindAll([System.Windows.Automation.TreeScope]::Descendants, (New-Object System.Windows.Automation.PropertyCondition($A::NameProperty, "StayWakeBlackScreenIdle vX.Y.Z - guarding")))).Count'
     ```

8. **Report** the release URL and, if installed, the version now running.

## Rules

- Never force-push, never skip hooks, never retag an existing version.
- Stop at the first failing step and say what broke; don't "fix" it by
  bypassing checks.
- Piping Windows output through `tr -d '\r'` keeps it readable from WSL.
- Don't launch a Windows GUI program with `cmd.exe /c start` from a command
  whose output you read — it inherits the pipe and the command hangs. Use
  `powershell.exe -NoProfile -Command "Start-Process ..."`.
