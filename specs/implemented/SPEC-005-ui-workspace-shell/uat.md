# SPEC-005 user acceptance test plan: UI stack and workspace shell

| | |
|---|---|
| **Build** | commit `5eed3e5` on `worktree-spec-005-draft` |
| **Written** | 2026-10-27 |
| **Testers** | Bench scientist, lab manager, QA reviewer (a developer may help with the terminal steps) |

All SPEC-005 requirements are `Test: no`, and the Playwright harness ships empty (no UI tests exist yet). This plan is therefore the only acceptance evidence for the UI. Everything is read-only in the UI: you create data with `curl`, then look at it in the browser.

Use a current Chrome, Edge or Firefox. Use the synthetic data below only.

## 1. Setup

Run from a clean checkout of the commit above. Steps use a bash shell (Git Bash or WSL on Windows). Node 22 and Go are needed.

```bash
docker compose up -d
make ui                      # npm ci && npm run build, puts the UI in web/dist
go run ./cmd/server          # API and UI on http://localhost:8080
```

Leave the server running. In a second terminal, in the same checkout, create the synthetic data:

```bash
export LAB=http://localhost:8080
H=(-H "X-LDE-Tenant: 00000000-0000-4000-8000-000000000001" -H "X-LDE-Actor: dev" -H "Content-Type: application/json")
first_id() { grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4; }

# Experiment with two protocol versions (so the diff has something to show)
EXP=$(curl -s "${H[@]}" -X POST $LAB/v1/experiments -d '{"slug":"il6-qpcr","title":"IL-6 qPCR time course (synthetic)","protocol_md":"---\nprotocol: qpcr-ddct\nversion: 1\nowner: molecular-biology\nreference_gene: GAPDH\n---\n\n# Relative quantification\n\n## Procedure\n1. Thaw master mix on ice.\n2. Plate triplicates.\n3. Run 40 cycles.\n"}' | first_id)
curl -s "${H[@]}" -X POST $LAB/v1/experiments/$EXP/protocol-versions -d '{"protocol_md":"---\nprotocol: qpcr-ddct\nversion: 2\nowner: molecular-biology\nreference_gene: GAPDH\n---\n\n# Relative quantification\n\n## Procedure\n1. Thaw master mix on ice.\n2. Plate triplicates.\n3. Run 45 cycles.\n4. Run a melt curve.\n"}' > /dev/null

# Samples, an instrument, and two runs
S1=$(curl -s "${H[@]}" -X POST $LAB/v1/lineage/nodes -d '{"kind":"sample","label":"PBMC donor 12 (synthetic)","attrs":{"matrix":"cDNA"}}' | first_id)
S2=$(curl -s "${H[@]}" -X POST $LAB/v1/lineage/nodes -d '{"kind":"sample","label":"PBMC donor 13 (synthetic)","attrs":{"matrix":"cDNA"}}' | first_id)
INST=$(curl -s "${H[@]}" -X POST $LAB/v1/instruments -d '{"name":"HPLC-01","kind":"hplc","attrs":{"serial":"SYN-0001","location":"Bench 3"}}' | first_id)
RUN=$(curl -s "${H[@]}" -X POST $LAB/v1/runs -d "{\"experiment_id\":\"$EXP\",\"label\":\"Plate 7\",\"params\":{\"cycles\":45,\"anneal_temp_c\":60},\"inputs\":[\"$S1\",\"$S2\"]}" | first_id)
RUN2=$(curl -s "${H[@]}" -X POST $LAB/v1/runs -d "{\"experiment_id\":\"$EXP\",\"label\":\"Plate 8\"}" | first_id)

# Plate 7: ingest readings, then take it to signed
curl -s "${H[@]/application\/json/application\/x-ndjson}" -X POST "$LAB/v1/instruments/$INST/stream?run_id=$RUN&label=HPLC-01%20stream" --data-binary @examples/streams/hplc-uv.ndjson
curl -s "${H[@]}" -X POST $LAB/v1/runs/$RUN/transitions -d '{"to":"running"}' > /dev/null
curl -s "${H[@]}" -X POST $LAB/v1/runs/$RUN/transitions -d '{"to":"completed"}' > /dev/null
curl -s "${H[@]}" -X POST $LAB/v1/runs/$RUN/transitions -d '{"to":"signed","reason":"Reviewed, results accepted"}' > /dev/null

# An experiment whose protocol contains a script tag (UAT-06)
curl -s "${H[@]}" -X POST $LAB/v1/experiments -d '{"slug":"xss-check","title":"Script tag check (synthetic)","protocol_md":"---\nprotocol: xss-check\nversion: 1\n---\n\n# Hostile protocol\n\n<script>document.title=\"PWNED\";alert(\"xss-uat\")</script>\n\n<img src=x onerror=\"alert('xss-img')\">\n\nBold text is **fine**.\n"}' > /dev/null

# 55 more experiments, so the explorer needs a second page (UAT-03)
for i in $(seq -w 1 55); do
  curl -s "${H[@]}" -X POST $LAB/v1/experiments -d "{\"slug\":\"zz-bulk-$i\",\"title\":\"Bulk $i (synthetic)\",\"protocol_md\":\"---\nprotocol: bulk\n---\n\nBulk $i\n\"}" > /dev/null
done
echo "EXP=$EXP RUN=$RUN RUN2=$RUN2 INST=$INST"
```

The ingest line should print a JSON body with `dataset_id`, `count` 5 or more, and a `sha256`. Note the `count` and `sha256`: UAT-04 compares them with the screen. If any other command printed an error, stop and ask a developer. Keep this terminal open: the cases below reuse `$LAB`, `$H`, `$EXP`, `$RUN`, `$RUN2` and `$INST`.

Open `http://localhost:8080/` in the browser. Open the browser developer tools (F12) for the cases that say so.

## 2. Test cases

### UAT-01: Sign in on a development server

- **Requirements:** SPEC-005-R09, SPEC-005-R14
- **As:** Bench scientist
- **Steps:**
  1. Open `http://localhost:8080/` in a private window (so no stored session exists).
  2. Look at the page. Check that the Tenant field is prefilled with `00000000-0000-4000-8000-000000000001`.
  3. Leave Actor empty and click Sign in.
  4. Type `alice` as Actor and click Sign in.
  5. In a terminal run `curl -s http://localhost:8080/v1/meta` (no headers).
  6. Click Sign out in the top bar, then reload the page.
  7. Sign in as `alice` again.
- **Expected:**
  - Step 2: a "Sign in" form with Tenant and Actor fields.
  - Step 3: the form does not submit (Actor is required).
  - Step 4: the workspace appears. The top bar shows `LabRat`, `alice`, `Ctrl+K` and a Sign out button. The explorer shows `il6-qpcr`, `xss-check` and `zz-bulk-…`.
  - Step 5: `{"env":"dev"}` with no identity headers needed.
  - Step 6: the sign-in form is shown again (the session was cleared).
  - Step 7: the workspace is back, and events you cause later show actor `dev` for the data you created with curl, not `alice`, because the UI only reads.
- **Result:** Pass / Fail · Notes:

### UAT-02: A production server shows no sign-in form

- **Requirements:** SPEC-005-R09, SPEC-005-R14
- **As:** Lab manager (with a developer for the terminal)
- **Steps:**
  1. Stop the dev server (Ctrl+C in its terminal).
  2. Start it in prod mode: `LDE_ENV=prod go run ./cmd/server`.
  3. Reload `http://localhost:8080/` in the window where you were signed in.
  4. Run `curl -s http://localhost:8080/v1/meta`.
  5. Run `curl -s -o /dev/null -w "%{http_code}\n" "${H[@]}" $LAB/v1/experiments`.
  6. Stop the server and start the normal dev server again (`go run ./cmd/server`).
- **Expected:**
  - Step 3: the text "Sign-in is not configured yet (SPEC-006)." and no Tenant or Actor fields, even though a stored session exists. No workspace.
  - Step 4: `{"env":"prod"}`.
  - Step 5: `501` (header identity is refused in prod).
  - Step 6: the dev sign-in form is back.
- **Result:** Pass / Fail · Notes:

### UAT-03: Browse the explorer and open items as tabs

- **Requirements:** SPEC-005-R05, SPEC-005-R10
- **As:** Bench scientist
- **Steps:**
  1. Signed in, look at the explorer on the left.
  2. Click the arrow beside `il6-qpcr`.
  3. Click the run `Plate 7 (signed)`, then `Plate 8 (planned)`.
  4. Click `Plate 7` in the explorer a second time.
  5. Click `il6-qpcr` (the name) and `HPLC-01` under Instruments.
  6. Scroll to the bottom of the experiment list. Click "Load more".
  7. Copy the browser address (it ends in `#/…`), open it in a new tab.
- **Expected:**
  - Step 1: about 50 experiments sorted by slug, then an "Instruments" group (expanded, showing `HPLC-01`) and "Audit trail". The list ends with a "Load more" row.
  - Step 2: the experiment expands and lists its runs with their state.
  - Step 3: each run opens in the tab strip as "Run: Plate 7" and "Run: Plate 8".
  - Step 4: no second Plate 7 tab opens; the existing tab is focused.
  - Step 5: "Experiment: il6-qpcr" and "Instrument: HPLC-01" tabs open. The address bar changes to `#/experiment/<id>`, `#/instrument/<id>` and so on, following the active tab.
  - Step 6: the remaining `zz-bulk-…` experiments (up to `zz-bulk-55`) are appended.
  - Step 7: the new browser tab opens straight to the same view.
  - There is no button anywhere to create, edit or transition anything.
- **Result:** Pass / Fail · Notes:

### UAT-04: Inspect a signed run and compare with the audit trail

- **Requirements:** SPEC-005-R06, SPEC-005-R13
- **As:** QA reviewer
- **Steps:**
  1. Open run `Plate 7`.
  2. Read State, Started, Finished, Protocol, Params, Inputs, Datasets and State history.
  3. Compare the Datasets row with the ingest output in the terminal.
  4. Run `curl -s "${H[@]}" $LAB/v1/runs/$RUN` and compare its `protocol_version`, `protocol_sha256`, `inputs` and `datasets` with the screen.
  5. Open run `Plate 8`.
- **Expected:**
  - Step 2: State is `signed`; Started and Finished have times. Protocol shows `v2` and a 64-character SHA-256. Params show `cycles 45` and `anneal_temp_c 60`. Inputs lists `sample: PBMC donor 12 (synthetic)` and `sample: PBMC donor 13 (synthetic)` (the protocol itself is not listed as an input). Datasets lists `HPLC-01 stream` with the reading count and SHA-256. State history lists the creation, the transitions to running, completed and signed (reason "Reviewed, results accepted"), with actor `dev` and times.
  - Step 3: the Readings number and SHA-256 equal the `count` and `sha256` printed at ingest. The screen shows the hash recorded when the data was ingested.
  - Step 4: the JSON agrees with the screen.
  - Step 5: State `planned`, "not started", "not finished", "No inputs", "No datasets", and a state history with only the creation event.
- **Result:** Pass / Fail · Notes:

### UAT-05: Read a protocol, its metadata and versions, and compare two versions

- **Requirements:** SPEC-005-R05, SPEC-005-R15
- **As:** Bench scientist
- **Steps:**
  1. Open experiment `il6-qpcr`.
  2. Read the title, version line, rendered protocol, Metadata table and Versions table.
  3. In "Compare versions" choose From `v1` and To `v2`, then click Compare.
  4. Choose From `v2` and To `v2` and click Compare.
  5. Open experiment `zz-bulk-01` and look at the Compare section.
- **Expected:**
  - Step 2: heading "IL-6 qPCR time course (synthetic)"; "Version 2" and its hash. The protocol is shown as formatted headings and a numbered list (the `---` frontmatter block is not repeated in the text). Metadata lists `protocol`, `version`, `owner`, `reference_gene`. Versions lists v1 and v2 with hash prefix, creator `dev` and time.
  - Step 3: a diff with removed lines marked `-` and added lines marked `+`: `3. Run 40 cycles.` replaced by `3. Run 45 cycles.`, a new `4. Run a melt curve.`, and `version: 1` replaced by `version: 2`.
  - Step 4: "No differences".
  - Step 5: "Needs two versions".
- **Result:** Pass / Fail · Notes:

### UAT-06: A protocol containing a script tag is shown as text, not run

- **Requirements:** SPEC-005-R04 (and safe rendering under R05)
- **As:** QA reviewer
- **Steps:**
  1. Open the browser developer tools Console and keep it open.
  2. Open experiment `xss-check`.
  3. Read the protocol area.
  4. Check the page title in the browser tab and look for any alert dialog.
  5. In the Console, look for messages mentioning Content Security Policy.
  6. Open the Elements panel and search (Ctrl+F) for `<script>alert` and `onerror`.
- **Expected:**
  - Steps 3-4: the heading "Hostile protocol", "Bold text is fine" in bold, and the script and image markup visible as literal text (or not shown as active content). No alert dialog appears. The page title is not "PWNED".
  - Step 5: no script was executed; the CSP produces no blocking message for the page's own code.
  - Step 6: any `<script>` text appears only as escaped text inside the protocol area, never as a live element, and the `<img>` has no `onerror` attribute.
- **Result:** Pass / Fail · Notes:

### UAT-07: View an instrument and open its lineage

- **Requirements:** SPEC-005-R16, SPEC-005-R05
- **As:** Bench scientist
- **Steps:**
  1. Open `HPLC-01` from the explorer.
  2. Read the name, Kind and Attributes.
  3. Click "Open lineage".
- **Expected:**
  - Step 2: heading `HPLC-01`, Kind `hplc`, attributes `serial SYN-0001` and `location Bench 3`.
  - Step 3: a lineage tab named `HPLC-01` opens (see UAT-08).
- **Result:** Pass / Fail · Notes:

### UAT-08: Explore lineage, switch direction and re-centre

- **Requirements:** SPEC-005-R07
- **As:** Bench scientist
- **Steps:**
  1. Open run `Plate 7`, click "Open lineage".
  2. Look at the graph and the direction control (Upstream, Downstream, Both).
  3. Choose Upstream, then Downstream, then Both.
  4. Click the node `PBMC donor 12 (synthetic)` in the graph.
  5. Click "Re-centre here".
  6. Zoom with the mouse wheel and drag the background.
- **Expected:**
  - Step 2: a left-to-right graph. With Both, it shows the two samples and the protocol version flowing into `Plate 7`, and `HPLC-01 stream` generated by `Plate 7`, with `HPLC-01` linked to the stream. The run node is outlined as the root. A side panel lists all nodes.
  - Step 3: Upstream shows what fed the run (samples and protocol, no dataset); Downstream shows the dataset and what leads onwards; Both shows the union. The graph redraws each time.
  - Step 4: the side panel shows kind `sample`, the label, created time and attributes (`"matrix": "cDNA"`).
  - Step 5: a new lineage tab opens centred on that sample; with Downstream you can see `Plate 7` and the dataset.
  - Step 6: zoom and pan work.
- **Result:** Pass / Fail · Notes:

### UAT-09: Page through, filter and verify the audit trail

- **Requirements:** SPEC-005-R08, SPEC-005-R10
- **As:** QA reviewer
- **Steps:**
  1. Click "Audit trail" in the explorer.
  2. Click "Verify chain".
  3. Look at the events table and click "Next page", then "First page".
  4. Enter Entity type `run` and the Plate 7 run id (`echo $RUN`) as Entity id, then click Filter.
  5. Click Clear.
  6. Enter Entity type `run` with an empty Entity id.
  7. Run `curl -s -o /dev/null -w "%{http_code}\n" "${H[@]}" "$LAB/v1/audit/events?entity_type=run"`.
- **Expected:**
  - Step 2: "Chain valid", an Engine head (seq and hash), a Database head (seq and hash), and "Engine and database agree". The two heads show the same seq and hash.
  - Step 3: columns Seq, Time, Actor, Action, Entity type, Entity id, Reason; 100 events per page in ascending seq order. Next page shows the following events; First page returns to seq 1.
  - Step 4: only events of that run (creation and the transitions, including the reason on the signature).
  - Step 5: the full list returns.
  - Step 6: the Filter button is disabled until both fields are filled.
  - Step 7: `400`.
- **Result:** Pass / Fail · Notes:

### UAT-10: Command palette

- **Requirements:** SPEC-005-R12
- **As:** Bench scientist
- **Steps:**
  1. With a view open, press Ctrl+K (Cmd+K on macOS).
  2. Type `plate`. Press the Down arrow once, then Enter.
  3. Press Ctrl+K, type `hplc`, press Enter.
  4. Press Ctrl+K, type `zzz-nothing`.
  5. Press Esc.
  6. Press Ctrl+K, type `verify`, press Enter.
  7. Press Ctrl+K, type `close all`, press Enter.
  8. Press Ctrl+K, type `sign out`, press Enter.
- **Expected:**
  - Step 1: a "Command palette" dialog with a search box in focus and a list.
  - Step 2: the list narrows to the runs `Plate 7` and `Plate 8` (each labelled Run), matching in any letter case. Enter opens the highlighted one as a tab. Runs only appear if you expanded their experiment in the explorer earlier (the palette uses what the explorer has loaded).
  - Step 3: `HPLC-01` opens as an instrument tab.
  - Step 4: "No matches".
  - Step 5: the dialog closes and focus returns to where it was before.
  - Step 6: the Audit trail tab opens and the chain verification runs, showing the result.
  - Step 7: all tabs close.
  - Step 8: you are signed out and the sign-in form appears.
  - The list always includes the commands Open audit trail, Verify audit chain, Close all tabs and Sign out.
- **Result:** Pass / Fail · Notes:

### UAT-11: Use every view with the keyboard only

- **Requirements:** SPEC-005-R11
- **As:** Bench scientist (and anyone who relies on the keyboard)
- **Steps:** Put the mouse aside. Sign in again if needed. Use Tab and Shift+Tab to move between regions.
  1. In the explorer, use Up/Down to move, Right to expand `il6-qpcr`, Left to collapse, Enter to open the item. Open a run, an experiment and `HPLC-01` this way.
  2. In the tab strip, use Left/Right to move between tabs, Enter to activate and Delete to close the focused tab. Try Ctrl+W as well (some browsers keep Ctrl+W for themselves; Delete is the fallback).
  3. In the experiment view, change the From and To versions with the keyboard and press Compare.
  4. In a lineage tab, Tab to the Direction control and change it with arrow keys. Tab to the "Nodes" list, move with Up/Down and press Enter to re-centre.
  5. In the audit trail, Tab to Verify chain and press Enter; fill the filter fields and submit with Enter; use Next page.
  6. In the bottom panel, Tab to Problems and History, switch with Left/Right.
  7. Open the palette with Ctrl+K, pick an item with Up/Down and Enter, and close one with Esc.
  8. Throughout, watch for a visible focus outline on whatever has focus.
  9. Check colour contrast: in Chrome or Edge open DevTools, run a Lighthouse "Accessibility" audit (or use an axe extension) on a run view and on the lineage view. Alternatively check the colour picker contrast ratio on body text, muted text, buttons and links.
- **Expected:**
  - Every action above works without a mouse. Focus is never lost to nowhere (after closing a tab focus lands on the tab strip; after Esc it returns to where it was).
  - A visible outline marks the focused control every time.
  - The bottom panel's History tab shows the active entity's events (try with a run open); Problems says "No problems".
  - Every icon-only button (for example the tab close "×") has an accessible name such as "Close Plate 7" (check in the DevTools Accessibility pane).
  - Lighthouse/axe reports no colour-contrast failures; text meets 4.5:1.
  - The lineage graph canvas itself is not keyboard reachable, but the Nodes list provides the same selection and re-centre.
- **Result:** Pass / Fail · Notes:

### UAT-12: A 500-node lineage renders within 2 seconds

- **Requirements:** SPEC-005 section 4 (non-functional: 500-node closure within 2 s)
- **As:** Bench scientist (on a 4-core laptop, or the nearest equivalent)
- **Steps:**
  1. Create the data (takes about a minute):
     ```bash
     BIG=$(curl -s "${H[@]}" -X POST $LAB/v1/lineage/nodes -d '{"kind":"analysis","label":"Big analysis (synthetic)"}' | first_id)
     for i in $(seq -w 1 499); do
       N=$(curl -s "${H[@]}" -X POST $LAB/v1/lineage/nodes -d "{\"kind\":\"sample\",\"label\":\"S-$i\"}" | first_id)
       curl -s "${H[@]}" -X POST $LAB/v1/lineage/edges -d "{\"from\":\"$N\",\"to\":\"$BIG\",\"relation\":\"used\"}" > /dev/null
     done
     echo "http://localhost:8080/#/lineage/$BIG"
     ```
  2. Close other heavy applications. Open the printed address in the browser (or paste it in the address bar of a tab already signed in).
  3. Open DevTools, Performance tab, start recording, reload the page, and stop once the graph is drawn. Alternatively use a stopwatch from the moment the `/v1/lineage/...` request finishes in the Network tab.
  4. Zoom and pan the graph, and click a node.
- **Expected:**
  - The lineage tab "Big analysis (synthetic)" shows 500 nodes (the side-panel Nodes list has 500 entries). The graph is laid out and drawn within 2 s of the response arriving.
  - The page stays responsive when zooming, panning and selecting.
  - Record the measured time in Notes.
- **Result:** Pass / Fail · Notes (time in seconds):

### UAT-13: The UI is served by the server alone, safely, and is small

- **Requirements:** SPEC-005-R01, SPEC-005-R03, SPEC-005-R04
- **As:** QA reviewer (with a developer for the terminal)
- **Steps:**
  1. Build the binary and run it from an unrelated folder, with the `web/` folder out of reach:
     ```bash
     go build -o /tmp/labrat ./cmd/server
     cd /tmp && LDE_HTTP_ADDR=:8082 ./labrat
     ```
     (stop the `go run` server first if you prefer one server at a time). Open `http://localhost:8082/`, sign in and open a run.
  2. If you can, disconnect the machine from the internet and reload.
  3. With DevTools Network tab open (tick "Disable cache"), reload `http://localhost:8080/` and click through an experiment, a run, lineage and the audit trail. Sort by Domain or look at the full URLs.
  4. In a terminal run `curl -i http://localhost:8080/`, `curl -sI http://localhost:8080/assets/` and `curl -i -X POST http://localhost:8080/` and `curl -i http://localhost:8080/nope`.
  5. Check the size of the gzipped bundle:
     ```bash
     cd web/dist/assets && for f in *; do echo "$f $(gzip -9c "$f" | wc -c)"; done
     ```
     Add the byte counts together.
- **Expected:**
  - Steps 1-2: the UI works fully from the binary alone, with no other web server and no internet.
  - Step 3: every request goes to the same origin (`localhost:8080`). API calls are only `/v1/meta`, `/v1/experiments`, `/v1/experiments/{id}/runs`, `/v1/instruments`, `/v1/runs/{id}`, `/v1/protocol-versions/{id}`, `/v1/experiments/{id}/protocol-versions`, `/v1/lineage/{id}`, `/v1/audit/events` and `/v1/audit/verify`. Each is an ordinary endpoint you can call with `curl` and the dev headers. No fonts, scripts, images or analytics come from another host.
  - Step 4: `/` returns 200 with `Content-Security-Policy: default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'` and `X-Content-Type-Options: nosniff` and `Cache-Control: no-cache`. A POST to `/` returns 405. `/nope` returns 404.
  - Step 5: the total gzipped size is under 1,000,000 bytes.
- **Result:** Pass / Fail · Notes:

### UAT-14: A server built without the UI says so

- **Requirements:** SPEC-005-R02
- **As:** Developer acting for the lab manager
- **Steps:**
  1. Make a fresh copy that has no built UI:
     ```bash
     git clone --branch worktree-spec-005-draft . /tmp/labrat-noui
     cd /tmp/labrat-noui && git checkout 5eed3e5
     go build -o labrat-noui ./cmd/server && echo BUILD OK
     LDE_HTTP_ADDR=:8083 ./labrat-noui
     ```
  2. Open `http://localhost:8083/`.
  3. In another terminal run `curl -s "${H[@]}" http://localhost:8083/v1/experiments`.
- **Expected:**
  - Step 1: the build succeeds without Node or `make ui`.
  - Step 2: a page saying "LabRat UI is not built. Run `make ui`, then rebuild the server."
  - Step 3: the API still answers with a normal JSON list.
- **Result:** Pass / Fail · Notes:

### UAT-15: The new API endpoints behave as documented

- **Requirements:** SPEC-005-R10, SPEC-005-R13, SPEC-005-R14, SPEC-005-R03
- **As:** QA reviewer (any other client of the API)
- **Steps:** Run each command and compare.
  1. `curl -s $LAB/v1/meta` (no headers).
  2. `curl -s "${H[@]}" "$LAB/v1/experiments?limit=2"`; then repeat adding `&cursor=<next_cursor from the first answer>`.
  3. `curl -s "${H[@]}" "$LAB/v1/experiments/$EXP/runs"`.
  4. `curl -s "${H[@]}" "$LAB/v1/instruments"`.
  5. `curl -s "${H[@]}" "$LAB/v1/runs/$RUN"`.
  6. `curl -s "${H[@]}" "$LAB/v1/audit/events?entity_type=run&entity_id=$RUN"` and `curl -s "${H[@]}" "$LAB/v1/audit/events?after=0&limit=2"`.
  7. Failures: `limit=0`, `limit=201`, `cursor=not-a-uuid` on `/v1/experiments`; `entity_id=$RUN` alone on `/v1/audit/events`; `/v1/experiments/00000000-0000-4000-8000-0000000000aa/runs`; `curl -s -o /dev/null -w "%{http_code}\n" $LAB/v1/experiments` (no headers).
- **Expected:**
  - Step 1: `{"env":"dev"}`.
  - Step 2: `{"items":[…2 experiments…],"next_cursor":"<id>"}` with `id`, `slug`, `title`, `created_at`, sorted by slug; the second call continues after the cursor with no repeats. A last page shorter than `limit` has `"next_cursor":null`.
  - Step 3: items `Plate 7` (signed) and `Plate 8` (planned) with `id`, `label`, `state`, `created_at`, oldest first.
  - Step 4: `HPLC-01` with `id`, `node_id`, `name`, `kind`, `attrs`, `created_at`.
  - Step 5: the run object plus `protocol_version` 2, `protocol_sha256`, `inputs` (two samples, ordered by label) and `datasets` (one, with `count` and `sha256` matching the ingest).
  - Step 6: only that run's events as a plain JSON array; the second call returns a plain array of 2 events (the array shape has not changed).
  - Step 7: `400` for each bad limit, cursor and for the lone `entity_id`; `404` for the unknown experiment; `401` without headers.
- **Result:** Pass / Fail · Notes:

## 3. Traceability

| Requirement | Cases | Automated test |
|---|---|---|
| SPEC-005-R01 | UAT-13 | — |
| SPEC-005-R02 | UAT-14 | — |
| SPEC-005-R03 | UAT-13, UAT-15 | — |
| SPEC-005-R04 | UAT-06, UAT-13 | — |
| SPEC-005-R05 | UAT-03, UAT-05, UAT-07 | — |
| SPEC-005-R06 | UAT-04 | — |
| SPEC-005-R07 | UAT-08 | — |
| SPEC-005-R08 | UAT-09 | — |
| SPEC-005-R09 | UAT-01, UAT-02 | — |
| SPEC-005-R10 | UAT-03, UAT-09, UAT-15 | — |
| SPEC-005-R11 | UAT-11 | — |
| SPEC-005-R12 | UAT-10 | — |
| SPEC-005-R13 | UAT-04, UAT-15 | — |
| SPEC-005-R14 | UAT-01, UAT-02, UAT-15 | — |
| SPEC-005-R15 | UAT-05 | — |
| SPEC-005-R16 | UAT-07 | — |
| Non-functional: 500-node lineage within 2 s | UAT-12 | — |
| Non-functional: gzipped bundle under 1 MB | UAT-13 | — |

No SPEC-005 requirement is `Test: yes`. The Playwright harness (`make e2e`) ships with no tests, so it exits successfully having run nothing; this is expected and not a UAT case.

## 4. Sign-off

| Tester | Role | Date | Outcome | Notes |
|---|---|---|---|---|
| | | | Accepted · Accepted with issues · Rejected | |
