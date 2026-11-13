# LeadLoop workflow and notification patch

Apply this patch to the matching LeadLoop project version. Copy the `backend`, `frontend`, `docs`, and `.env.example` paths into the project root, preserving folders. Do not copy any `.env` file from an archive.

## Included changes

- Per-user live notification stream (SSE) with automatic reconnect and a slower refresh fallback.
- Notifications for new leads, inbound customer messages while in staff mode, new follow-up drafts, campaign resubmissions, approvals, and rejections.
- Content submission history with a version number and latest submitter, including a migration baseline for older non-draft content.
- Manual follow-up draft creation, creator attribution, consent checks, and approval-required workflow.
- LINE Official Account credentials can be entered from the admin integration page. The API verifies the bot profile and encrypts Channel Secret and Access Token with AES-256-GCM before storing them in PostgreSQL. Raw credentials are not returned by the status API.
- A single LeadLoop workspace is supported by this credential store. This patch does not implement multi-company tenant isolation or LINE OAuth onboarding.
- Campaigns are ordered by latest content activity; leads with the newest customer message or creation time move to the top; approvals, follow-ups, and notifications are newest-first.

## Required setup for in-app LINE credentials

1. Set `INTEGRATION_ENCRYPTION_KEY` in the API server environment to a private random string of exactly 32 characters. Generate one in PowerShell:

   ```powershell
   -join ((48..57) + (65..90) + (97..122) | Get-Random -Count 32 | ForEach-Object { [char]$_ })
   ```
2. Keep this master key in the server secret store and back it up securely. It is not the LINE Channel Secret or Access Token. Losing it means stored credentials cannot be decrypted.
3. Rebuild/restart the API. In the admin UI, open **การเชื่อมต่อระบบ** and connect the company's LINE Official Account.
4. Set the public HTTPS webhook URL in LINE Developers and verify it there.

If the existing project already uses `LINE_CHANNEL_SECRET` and `LINE_CHANNEL_ACCESS_TOKEN` in `.env`, setting the new encryption key switches the app to the encrypted database credentials. Re-enter those credentials in the integration page after enabling the key.

## Build and verification

- Backend files were formatted with `gofmt`.
- A full `go test ./...` could not be completed in this environment because Go dependencies could not be downloaded (network/DNS unavailable).
- Frontend production build could not be completed here because frontend dependencies are not installed in this environment.
- Run these checks on the project machine after applying the patch:

  ```powershell
  docker compose up -d --build api
  docker compose logs --tail=100 api
  cd frontend
  npm install
  npm run build
  npm run dev
  ```

Test with two accounts: submit a campaign, then resubmit a rejected item; the approver should see a live bell update and the revision label. Create a manual follow-up; it should appear as pending approval and show its author. Test LINE connection with the company's test Official Account before using real customers.
