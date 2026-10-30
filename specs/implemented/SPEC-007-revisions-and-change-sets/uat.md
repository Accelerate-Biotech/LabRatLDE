# SPEC-007 user acceptance test plan: Revisions, change sets and notes

| | |
|---|---|
| **Build** | commit `88c2e64` on `worktree-spec-007-revisions` |
| **Written** | 2026-10-30 |
| **Testers** | Bench scientist (alice), a second scientist or reviewer (bob), lab manager / QA checking the Part 11 controls (a developer will help with the database steps in UAT-13) |

Eight of the thirteen requirements have automated tests (R01 to R06, R10, R13). R07, R08, R09, R11 and R12 are `Test: no`: this plan is the only acceptance evidence for them.

**The UI has not been checked in a browser by the builders.** The edit-with-reason forms, the "changed" marker, the Revisions panel, the notes section and the review inbox have only been type-checked and built. UAT-02, UAT-03, UAT-04, UAT-05, UAT-08, UAT-09, UAT-11 and UAT-12 are therefore the first time a person sees them. Follow the UI steps literally and write down anything that looks wrong, even if it is not mentioned under Expected (layout, stale values after saving, lost focus, missing labels).

Use synthetic data only (the names and samples below). Use a current Chrome, Edge or Firefox.

## 1. Setup

From a clean checkout of the commit above, in a bash shell (Git Bash or WSL on Windows). Node 22, Go, Docker and `jq` are needed.

```bash
docker compose up -d
make ui                      # npm ci && npm run build, puts the UI in web/dist
go run ./cmd/server          # API and UI on http://localhost:8080, LDE_ENV=dev
```

The first log line must say `"env":"dev"`. Leave the server running. For a clean slate you can first run `docker compose down -v` (this deletes the local development database).

In a second terminal, in the same checkout, define the helpers and create the synthetic data. In development, a new handle is created on first use and holds every role, so `alice`, `bob` and `bot` need no provisioning for people (the agent is made in step 4).

```bash
export LAB=http://localhost:8080
export T=00000000-0000-4000-8000-000000000001          # seeded dev tenant

# as USER METHOD PATH [JSON]  -> prints "body [STATUS]".  Prefix with OBO=alice to act as an agent for alice.
as() {
  local u=$1 m=$2 p=$3 b=${4:-}
  curl -s -w ' [%{http_code}]\n' -X "$m" -H "X-LDE-Tenant: $T" -H "X-LDE-Actor: $u" \
    ${OBO:+-H "X-LDE-On-Behalf-Of: $OBO"} -H 'Content-Type: application/json' ${b:+-d "$b"} "$LAB$p"
}
field() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4; }     # field NAME  <  response  -> first string value
events() { as alice GET '/v1/audit/events?after=0&limit=500' | sed 's/ \[[0-9]*\]$//'; }
nevents() { events | jq length; }

# 1. An experiment, and the samples
EXP=$(as alice POST /v1/experiments '{"slug":"plasma-prep","title":"Plasma prep (synthetic)","protocol_md":"---\nprotocol: plasma-prep\nversion: 1\n---\n\n# Steps\n1. Thaw on ice.\n"}' | field id)
S=$(as alice POST /v1/lineage/nodes '{"kind":"sample","label":"Plasma 042","attrs":{"volume_ul":50}}' | field id)
S3=$(as alice POST /v1/lineage/nodes '{"kind":"sample","label":"Cycle A (synthetic)"}' | field id)
S4=$(as alice POST /v1/lineage/nodes '{"kind":"sample","label":"Cycle B (synthetic)"}' | field id)

# 2. Run B stays open (planned) and uses sample S. Run A is taken to signed (sealed).
RUNB_JSON=$(as alice POST /v1/runs "{\"experiment_id\":\"$EXP\",\"label\":\"Plate 8\",\"params\":{\"cycles\":40,\"anneal_temp_c\":60},\"inputs\":[\"$S\"]}")
RUNB=$(echo "$RUNB_JSON" | field id); RUNB_NODE=$(echo "$RUNB_JSON" | field node_id)
RUNA_JSON=$(as alice POST /v1/runs "{\"experiment_id\":\"$EXP\",\"label\":\"Plate 7\",\"params\":{\"cycles\":45}}")
RUNA=$(echo "$RUNA_JSON" | field id); RUNA_NODE=$(echo "$RUNA_JSON" | field node_id)

# 3. A note on run A before it is signed, then sign run A (this seals it)
NOTE_PRE=$(as alice POST /v1/lineage/nodes/$RUNA_NODE/notes '{"body":"Plate sealed with foil before the run."}' | field id)
as alice POST /v1/runs/$RUNA/transitions '{"to":"running"}'
as alice POST /v1/runs/$RUNA/transitions '{"to":"completed"}'
as bob   POST /v1/runs/$RUNA/transitions '{"to":"signed","reason":"Reviewed, results accepted"}'

# 4. An agent "bot" that may act for alice (needed from UAT-06)
BOT=$(as alice POST /v1/principals '{"kind":"agent","handle":"bot","display_name":"Assay bot (synthetic)","issuer":"dev","subject":"bot","reason":"UAT agent"}' | field id)
ALICE=$(as alice GET /v1/principals | sed 's/ \[[0-9]*\]$//' | jq -r '.items[] | select(.handle=="alice") | .id')
as alice POST /v1/role-grants "{\"principal_id\":\"$BOT\",\"role\":\"scientist\",\"reason\":\"UAT agent\"}"
as alice POST /v1/agent-delegations "{\"agent_id\":\"$BOT\",\"human_id\":\"$ALICE\",\"reason\":\"UAT automation for alice\"}"

echo "EXP=$EXP S=$S S3=$S3 S4=$S4 RUNA=$RUNA RUNA_NODE=$RUNA_NODE RUNB=$RUNB RUNB_NODE=$RUNB_NODE NOTE_PRE=$NOTE_PRE BOT=$BOT"
```

Every id must be non-empty and every call that prints a status must show `[200]` or `[201]`. If not, stop and ask a developer. Keep this terminal: the cases reuse these variables.

For the browser, open `http://localhost:8080/` in a **private window**. Sign in with Tenant `00000000-0000-4000-8000-000000000001` (prefilled) and Handle `alice`. UAT-09 and UAT-12 need a second private window signed in as `bob`.

To open records in the UI: in the explorer, open experiment `plasma-prep`, then its run `Plate 8` (run B) or `Plate 7` (run A). In a run view, "Open lineage" opens that run's lineage. For sample `Plasma 042`, open run B's lineage, and choose `Plasma 042` in the Nodes list.

## 2. Test cases

### UAT-01: Correct a mistyped volume and keep the old value

- **Requirements:** SPEC-007-R01, SPEC-007-R02 (blank reason), SPEC-007-R11
- **As:** Bench scientist (alice)
- **Steps:**
  1. Edit the sample's attributes without a reason:
     `as alice PATCH /v1/lineage/nodes/$S '{"attrs":{"volume_ul":45}}'` and again with `"reason":"   "`.
  2. Edit with a reason:
     `as alice PATCH /v1/lineage/nodes/$S '{"attrs":{"volume_ul":45},"reason":"pipetting error"}'`. Write down `change_set_id` as `CS1`.
  3. List the history: `as alice GET /v1/records/lineage_node/$S/revisions`
  4. Read the original: `as alice GET /v1/records/lineage_node/$S/revisions/1`
  5. See the current value: `as alice GET "/v1/lineage/$RUNB_NODE?direction=upstream"` and find `Plasma 042`.
  6. See the change set that the edit made: `as alice GET /v1/change-sets/$CS1`
  7. Look at the audit trail: `events | jq -r '.[] | select(.action=="record.revised" or .action=="change_set.committed") | [.seq,.actor,.action,.reason] | @tsv'`
- **Expected:**
  - Step 1: both calls return `[400]` and an `error` about the reason. Nothing changed (step 3 still shows one revision).
  - Step 2: `[200]` with `{"change_set_id":"…","rev":2}`.
  - Step 3: two revisions, oldest first. Rev 1: label `Plasma 042`, attrs `{"volume_ul":50}`, author `alice`, reason `created`. Rev 2: attrs `{"volume_ul":45}`, label still `Plasma 042`, author `alice`, reason `pipetting error`, `change_set_id` equal to `CS1`.
  - Step 4: the full rev 1 value with `volume_ul` 50. The earlier value is still readable.
  - Step 5: the node's attrs show `45`.
  - Step 6: `state` `committed`, title `Edit lineage_node`, `source` `human`, author `alice`, one item (`update_node`), `decision_reason` `pipetting error`, `decided_by` `alice`.
  - Step 7: a `change_set.committed` and a `record.revised` event by `alice`, reason `pipetting error`. Creating the sample produced no `record.revised` (its creation event covers revision 1).
- **Result:** Pass / Fail · Notes:

### UAT-02: Correct a run's parameters in the UI

- **Requirements:** SPEC-007-R01, SPEC-007-R09 (changed marker), SPEC-007-R11
- **As:** Bench scientist (alice)
- **Steps:**
  1. In the browser, open run `Plate 8`. Under **Params** read the value (JSON with `cycles` 40 and `anneal_temp_c` 60). Check that no "changed" button is shown.
  2. Click **Edit** next to Params. A text area with the JSON and a **Reason** field appear.
  3. Replace the text with `not json`, enter any reason, click **Save**.
  4. Replace the text with `[1, 2]`, click **Save**.
  5. Replace the text with `{"cycles": 40, "anneal_temp_c": 62}` and clear the Reason. Look at the Save button.
  6. Enter the reason `calibrated thermocycler` and click **Save**.
  7. Click the **changed** button beside Params.
  8. Check the history from the terminal: `as alice GET /v1/records/run/$RUNB/revisions`.
- **Expected:**
  - Step 1: Params shows the JSON, with an Edit button and no "changed" button.
  - Step 3: a red message "Value must be valid JSON." and nothing is saved.
  - Step 4: "Value must be a JSON object."
  - Step 5: Save is disabled while the reason is empty.
  - Step 6: the form closes, focus returns to the Edit button, Params shows `anneal_temp_c` 62, and a **changed** button appears beside it.
  - Step 7: the bottom panel switches to its **Revisions** tab for this run (see UAT-05 for what the tab shows).
  - Step 8: two revisions; rev 2 has author `alice`, reason `calibrated thermocycler`.
  - If the screen still shows the old value or no "changed" button after saving, record that as a failure.
- **Result:** Pass / Fail · Notes:

### UAT-03: Correct a sample's label and attributes in the UI

- **Requirements:** SPEC-007-R01, SPEC-007-R09 (changed marker per field), SPEC-007-R11
- **As:** Bench scientist (alice)
- **Steps:**
  1. Open run `Plate 8`, click "Open lineage", and select `Plasma 042` in the Nodes list. The **Selected node** panel shows **Label** and **Attributes**, each with an Edit button. Attributes already hold the value from UAT-01, so Attributes may already show a "changed" button; Label must not.
  2. Edit **Label**: set it to `Plasma 042 (thawed)` with the reason `thaw logged` and click Save.
  3. Edit **Attributes**: enter `{"volume_ul": 45, "donor": "D-17"}` with the reason `donor recorded` and Save.
  4. Edit **Attributes** and try `{`, then `[1]`.
- **Expected:**
  - Step 2: Label shows the new text and gets a **changed** button.
  - Step 3: Attributes shows the new JSON with a **changed** button. The marker on each field appears only when that field has been changed after revision 1.
  - Step 4: "Value must be valid JSON." and "Value must be a JSON object." No revision is added.
  - Clicking either **changed** button opens the Revisions tab, which should list three or more revisions for this node.
- **Result:** Pass / Fail · Notes:

### UAT-04: Correct an experiment's title in the UI

- **Requirements:** SPEC-007-R01, SPEC-007-R09 (changed marker), SPEC-007-R11
- **As:** Bench scientist (alice)
- **Steps:**
  1. Open experiment `plasma-prep`. Find **Title** with its Edit button.
  2. Edit it to `Plasma preparation (synthetic)` with the reason `fix title`, and Save.
  3. Edit it again and try to save with an empty reason.
  4. From the terminal, try to blank the title: `as alice PATCH /v1/experiments/$EXP '{"title":"  ","reason":"x"}'`
- **Expected:**
  - Step 2: the new title shows with a **changed** button; the Revisions tab lists two revisions (`created`, `fix title`).
  - Step 3: Save stays disabled.
  - Step 4: `[400]`, and the title is unchanged.
- **Result:** Pass / Fail · Notes:

### UAT-05: See what changed, by whom, and when

- **Requirements:** SPEC-007-R07, SPEC-007-R08, SPEC-007-R09
- **As:** Reviewer / QA (bob)
- **Steps:**
  1. Make one more edit to the sample so the history has an unusual key: `as alice PATCH /v1/lineage/nodes/$S '{"attrs":{"volume_ul":45,"donor":"D-17","read-out":1},"reason":"add read-out"}'`
  2. From the terminal: `as bob GET /v1/records/lineage_node/$S/revisions` (oldest first), then `as bob GET /v1/records/lineage_node/$S/revisions/99` and `as bob GET /v1/records/lineage_node/$S/diff?from=1&to=99`.
  3. `as bob GET "/v1/records/lineage_node/$S/diff?from=1&to=4"` and `as bob GET /v1/records/lineage_node/$S/blame`.
  4. In a private window signed in as `bob`, open run `Plate 8`, "Open lineage", select `Plasma 042`, and open the bottom panel's **Revisions** tab (beside Problems and History).
  5. Read the revision table. It defaults to the previous and latest revision. Choose From revision 1 and To revision 4.
  6. Hover over each path in the Changes table.
  7. Choose the same revision for From and To.
  8. Switch to another record (open experiment `plasma-prep`) while the Revisions tab is open.
- **Expected:**
  - Step 2: revisions are in rev order 1, 2, 3, 4, each with `value`, `author`, `reason`, `change_set_id`, `created_at`. Revision 99 returns `[404]`, and so does the diff.
  - Step 3: `changes` list paths such as `$.label` (changed), `$.attrs.donor` (added, `to` `"D-17"`), `$.attrs["read-out"]` (added, the key is written in brackets because of the hyphen) and `$.attrs.volume_ul` (changed, 50 to 45). Blame lists each leaf (`$.label`, `$.attrs.volume_ul`, `$.attrs.donor`, `$.attrs["read-out"]`) with `rev`, `author` and `at`: `volume_ul` blames rev 2 and `read-out` blames rev 4.
  - Step 4 and 5: a table with columns From, To, Rev, Time, Author, Reason, with one row per revision, oldest first. The Changes table (Path, Kind, From, To) shows the same changes as the terminal.
  - Step 6: each path shows a tooltip like `rev 2 · alice · <date and time>`, matching the blame from step 3.
  - Step 7: "No differences".
  - Step 8: the panel now shows the experiment's revisions.
- **Result:** Pass / Fail · Notes:

### UAT-06: A proposed change changes nothing until it is committed

- **Requirements:** SPEC-007-R05
- **As:** Bench scientist (alice), with an agent (bot) proposing for her
- **Steps:**
  1. Note the current label: `as alice GET "/v1/lineage/$RUNB_NODE?direction=upstream"` (the sample is shown with its label) and `nevents` (the number of audit events). Call the count `N0`.
  2. The agent proposes two changes, acting for alice:
     ```bash
     OBO=alice as bot POST /v1/change-sets "{\"title\":\"Relabel plasma and add aliquot\",\"reason\":\"align with freezer inventory\",\"items\":[{\"op\":\"update_node\",\"node_id\":\"$S\",\"label\":\"P-042\"},{\"op\":\"add_node\",\"kind\":\"sample\",\"label\":\"Plasma 042 aliquot B\"}]}"
     ```
     Write down the returned `id` as `CS_BOT`.
  3. Look at the sample again (step 1 call), then `as alice GET /v1/records/lineage_node/$S/revisions`.
  4. Check the audit trail for the new node: `events | jq -r '.[] | select(.action=="lineage.node_created") | .payload'`.
  5. Read the proposal: `as alice GET /v1/change-sets/$CS_BOT`.
- **Expected:**
  - Step 2: `[201]`, `state` `proposed`, `source` `agent`, `author` `alice via bot`, `items_sha256` present.
  - Step 3: the sample keeps its current label (not `P-042`), and the revision list has no new revision.
  - Step 4: no `lineage.node_created` event for `Plasma 042 aliquot B`; `nevents` has grown by one only (a `change_set.proposed` event).
  - Step 5: `current` has one entry per item: for the first item the sample's latest value (label and attrs), for the second `null`.
  - The change set must be committed in UAT-08. Keep it proposed until then.
- **Result:** Pass / Fail · Notes:

### UAT-07: An agent cannot approve or reject its own proposal

- **Requirements:** SPEC-007-R06
- **As:** Bench scientist (alice) and QA
- **Steps:**
  1. The agent tries to commit its own proposal: `OBO=alice as bot POST /v1/change-sets/$CS_BOT/commit '{"reason":"self approve"}'`
  2. The agent tries to reject it: `OBO=alice as bot POST /v1/change-sets/$CS_BOT/reject '{"reason":"self reject"}'`
  3. Read the change set: `as alice GET /v1/change-sets/$CS_BOT`.
- **Expected:**
  - Steps 1 and 2: `[403]` with an `error`.
  - Step 3: `state` is still `proposed`, `decided_by` is `null`, and the sample's label is still unchanged.
- **Result:** Pass / Fail · Notes:

### UAT-08: Review and commit a proposal in the inbox

- **Requirements:** SPEC-007-R05 (visible after commit), SPEC-007-R12 (UI side)
- **As:** Bench scientist (alice)
- **Steps:**
  1. Alice also proposes a change herself, so the groups can be compared:
     `as alice POST /v1/change-sets "{\"title\":\"Fix run B cycles\",\"reason\":\"protocol says 42\",\"items\":[{\"op\":\"update_run_params\",\"run_id\":\"$RUNB\",\"params\":{\"cycles\":42,\"anneal_temp_c\":62}}]}"`. Write down `id` as `CS_ALICE`.
  2. In the browser as alice, open the **Review inbox** from the explorer. Close it and open it again with Ctrl+K and "Review inbox".
  3. Read the page: headings **Human**, **Parser** and **Agent**.
  4. Expand "Relabel plasma and add aliquot" (the agent's card) by clicking its title.
  5. Click **Commit**, leave the reason empty and look at the Confirm button, then type `matches inventory` and click **Confirm commit**.
  6. Open the sample in the lineage view (UAT-03 steps) and read its label. Then check the audit trail for the new node (UAT-06 step 4).
  7. Back in the inbox, expand "Fix run B cycles".
- **Expected:**
  - Step 2: the inbox opens as a single tab (the second open focuses the existing tab).
  - Step 3: the agent's card is under **Agent** and shows author `alice via bot`, the reason, and "2 items". Alice's card is under **Human**. **Parser** shows "None."
  - Step 4: item 1 `update_node` is a table (Field, Current, Proposed) with `label`, current `"Plasma 042 (thawed)"` (or the label from UAT-03), proposed `"P-042"`. Item 2 reads "add sample Plasma 042 aliquot B".
  - Step 5: Confirm is disabled until a reason is typed. After confirming the card disappears and the heading "Review inbox" gets focus.
  - Step 6: the label is `P-042`, there is a `lineage.node_created` event for `Plasma 042 aliquot B` and a `record.revised` event for the sample. The revision list shows a new revision whose reason is the **proposal's** reason (`align with freezer inventory`), author `alice via bot`.
  - Step 7: item 1 shows `params` with Current `{"anneal_temp_c":62,"cycles":40}` (or the value from UAT-02) and Proposed `{"anneal_temp_c":62,"cycles":42}`. Alice's own card offers a **Withdraw** button as well as Commit and Reject. The agent's card (step 4) offered Commit and Reject only if alice was not its author; note what you saw.
  - Leave `CS_ALICE` proposed for UAT-09.
- **Result:** Pass / Fail · Notes:

### UAT-09: Only the author can change or withdraw a proposal, and decisions are final

- **Requirements:** SPEC-007-R12
- **As:** Bench scientist (alice) and a second scientist (bob)
- **Steps:**
  1. Bob tries to replace alice's proposal:
     `as bob PUT /v1/change-sets/$CS_ALICE "{\"title\":\"Hijack\",\"reason\":\"x\",\"items\":[{\"op\":\"set_experiment_title\",\"experiment_id\":\"$EXP\",\"title\":\"Hijacked\"}]}"`
  2. Bob tries to withdraw it: `as bob POST /v1/change-sets/$CS_ALICE/withdraw '{"reason":"not mine"}'`
  3. Alice replaces her own items:
     `as alice PUT /v1/change-sets/$CS_ALICE "{\"title\":\"Fix run B cycles\",\"reason\":\"protocol says 42\",\"items\":[{\"op\":\"update_run_params\",\"run_id\":\"$RUNB\",\"params\":{\"cycles\":42,\"anneal_temp_c\":63}}]}"`
  4. In bob's browser window open the Review inbox and expand "Fix run B cycles". Note which buttons are shown. Then, in alice's window (reload the inbox), expand the same card and note the buttons.
  5. In bob's window click **Reject**, enter the reason `use 40 cycles`, and confirm.
  6. Try to commit and to withdraw the rejected change set:
     `as alice POST /v1/change-sets/$CS_ALICE/commit '{"reason":"too late"}'` and `as alice POST /v1/change-sets/$CS_ALICE/withdraw '{"reason":"too late"}'`.
  7. Alice proposes another change, `as alice POST /v1/change-sets "{\"title\":\"Throwaway\",\"reason\":\"test\",\"items\":[{\"op\":\"set_experiment_title\",\"experiment_id\":\"$EXP\",\"title\":\"Throwaway\"}]}"` (note `id` as `CS_W`). In alice's inbox withdraw it with the reason `not needed`.
  8. Try to reject it afterwards: `as bob POST /v1/change-sets/$CS_W/reject '{"reason":"late"}'`, and try an empty reason on any proposed change set: `as alice POST /v1/change-sets/$CS_ALICE/withdraw '{"reason":" "}'`.
- **Expected:**
  - Steps 1 and 2: `[403]`. The proposal is unchanged.
  - Step 3: `[200]`; the change set holds the new items (`anneal_temp_c` 63). A `change_set.updated` audit event exists.
  - Step 4: bob sees only **Commit** and **Reject**. Alice sees **Commit**, **Reject** and **Withdraw**. The Current column shows the run's present params.
  - Step 5: the card leaves the inbox. `as alice GET /v1/change-sets/$CS_ALICE` shows `state` `rejected`, `decided_by` `bob`, `decision_reason` `use 40 cycles`, and run B's params did not change.
  - Step 6: both return `[409]` (the change set is rejected).
  - Step 7: the card leaves the inbox; the state is `withdrawn`.
  - Step 8: `[409]` for the withdrawn one. An empty reason returns `[400]` (or `[409]` since `CS_ALICE` is already rejected; the point is that nothing is written).
  - The experiment's title is not `Hijacked` or `Throwaway`.
- **Result:** Pass / Fail · Notes:

### UAT-10: A commit applies everything or nothing

- **Requirements:** SPEC-007-R04
- **As:** Bench scientist (alice)
- **Steps:**
  1. Note the sample's revision count (`as alice GET /v1/records/lineage_node/$S/revisions | jq` or count the entries) and `nevents` (`N1`).
  2. Alice proposes three items, the last two of which make a loop:
     ```bash
     as alice POST /v1/change-sets "{\"title\":\"Loop test\",\"reason\":\"UAT atomicity\",\"items\":[{\"op\":\"update_node\",\"node_id\":\"$S\",\"label\":\"SHOULD NOT APPEAR\"},{\"op\":\"add_edge\",\"from\":\"$S3\",\"to\":\"$S4\",\"relation\":\"wasDerivedFrom\"},{\"op\":\"add_edge\",\"from\":\"$S4\",\"to\":\"$S3\",\"relation\":\"wasDerivedFrom\"}]}"
     ```
     Write down `id` as `CS_LOOP`.
  3. Try to commit it from the terminal: `as alice POST /v1/change-sets/$CS_LOOP/commit '{"reason":"try"}'`.
  4. Check the sample's label and revision count again, and `nevents`.
  5. Try the same commit from the browser: open the inbox, expand "Loop test", Commit with the reason `try`.
  6. Reject it in the inbox (reason `abandon loop test`).
- **Expected:**
  - Step 3: `[409]` with an `error`.
  - Step 4: the label is not `SHOULD NOT APPEAR`, the revision count is unchanged, and `nevents` is only `N1 + 1` (the `change_set.proposed` event). No edge or `change_set.committed` event exists for it.
  - Step 5: a red message appears in the card (the error text from the server). The card stays in the inbox, still proposed.
  - Step 6: the card leaves the inbox.
- **Result:** Pass / Fail · Notes:

### UAT-11: A signed run cannot be corrected yet, but can be annotated

- **Requirements:** SPEC-007-R10, SPEC-007-R13
- **As:** Bench scientist (alice)
- **Steps:**
  1. Run A (`Plate 7`) was signed during setup. Try a direct edit of its params: `as alice PATCH /v1/runs/$RUNA/params '{"params":{"cycles":50},"reason":"late fix"}'`
  2. Propose the same edit as a change set and try to commit it:
     ```bash
     CS_SEAL=$(as alice POST /v1/change-sets "{\"title\":\"Fix run A cycles\",\"reason\":\"late fix\",\"items\":[{\"op\":\"update_run_params\",\"run_id\":\"$RUNA\",\"params\":{\"cycles\":50}}]}" | field id)
     as alice POST /v1/change-sets/$CS_SEAL/commit '{"reason":"try"}'
     ```
  3. Try to edit the note written before the signing: `as alice PATCH /v1/notes/$NOTE_PRE '{"body":"Foil seal removed.","reason":"late fix"}'`
  4. Add a note to the sealed run: `as alice POST /v1/lineage/nodes/$RUNA_NODE/notes '{"body":"Freezer alarm at 02:10, samples unaffected"}'`. Write down `id` as `NOTE_ANN`.
  5. Read the run: `as alice GET /v1/runs/$RUNA`.
  6. Edit the annotation: `as alice PATCH /v1/notes/$NOTE_ANN '{"body":"Freezer alarm at 02:10, samples unaffected (checked)","reason":"clarify"}'`
  7. In the browser, open run `Plate 7` and scroll to **Notes**. Click **Edit** on the first note (the pre-signing note), enter a body and reason, and Save.
  8. Reject the proposal from step 2 in the inbox (reason `sealed`), to leave the inbox clean.
- **Expected:**
  - Step 1: `[409]`; params stay `{"cycles":45}`.
  - Step 2: the proposal is created (`[201]`); the commit returns `[409]`; the change set stays `proposed`.
  - Step 3: `[409]`; the note is unchanged.
  - Step 4: `[201]` with `"annotation":true`.
  - Step 5: `state` is still `signed`.
  - Step 6: `[200]`, `rev` 2.
  - Step 7: the Notes section lists the pre-signing note first and the annotation second. The annotation carries the label "Annotation (added after sealing)" and, after step 6, "edited (rev 2)". The pre-signing note has no annotation label. Saving the edit to the pre-signing note shows a red error and the text is not changed.
- **Result:** Pass / Fail · Notes:

### UAT-12: Write and correct notes in the UI

- **Requirements:** SPEC-007-R13 (notes on an unsealed node), SPEC-007-R01
- **As:** Bench scientist (alice); a second scientist (bob) for step 6
- **Steps:**
  1. In the browser, open run `Plate 8` and scroll to **Notes**. Read the empty state.
  2. In **Add note**, type `Samples looked **slightly cloudy**.` and, on a new line, `<script>alert("xss")</script>`. Click **Preview**, read the preview, then click **Preview** again and **Save**.
  3. Try to save an empty note (clear the text area).
  4. Click **Edit** on the note, change the text to `Samples looked **clear** after centrifugation.` with the reason `re-checked`, and Save.
  5. Click **Edit** again and try to save with an empty reason.
  6. In bob's window open the same run and add a note `Plate re-read at 14:00.`.
  7. Check from the terminal: `as alice GET /v1/lineage/nodes/$RUNB_NODE/notes` and the note's history, using the `id` of the first note: `as alice GET /v1/records/note/<note id>/revisions`.
- **Expected:**
  - Step 1: "No notes yet." and an Add note text area.
  - Step 2: the preview shows "cloudy" in bold; the script line appears as plain text and no alert dialog opens. After saving, the note is listed with author `alice`, a time, and no annotation label.
  - Step 3: Save is disabled when the text is empty.
  - Step 4: the note shows "clear" in bold and the tag "edited (rev 2)".
  - Step 5: Save stays disabled.
  - Step 6: after bob's note, alice sees two notes oldest first once she reopens the view. Neither is marked as an annotation (the run is not sealed).
  - Step 7: the notes list has `annotation` false and `rev` 2 for the edited note, `rev` 1 for bob's. The note history has revision 1 (original text, reason `created`) and revision 2 (reason `re-checked`).
- **Result:** Pass / Fail · Notes:

### UAT-13: The database itself protects the history

- **Requirements:** SPEC-007-R02, SPEC-007-R03
- **As:** QA / validation staff (with a developer; direct database access as the database owner)
- **Steps:**
  1. Open a database shell: `docker compose exec postgres psql -U lde lde` (use the user and database from your `LDE_DATABASE_URL` if they differ). Replace `<S>` with the value of `$S`, `<CS1>` with the id from UAT-01, and `<CS_PROPOSED>` with the id of a change set that is still proposed (create one with `as alice POST /v1/change-sets …` if none is left).
  2. Change a revisable field with no change set:
     ```sql
     UPDATE lineage_nodes SET attrs = '{}' WHERE id = '<S>';
     ```
  3. Name a change set that is only proposed:
     ```sql
     BEGIN;
     SELECT set_config('app.change_set_id', '<CS_PROPOSED>', true);
     UPDATE lineage_nodes SET attrs = '{}' WHERE id = '<S>';
     ROLLBACK;
     ```
  4. Name a change set that was committed in an earlier transaction:
     ```sql
     BEGIN;
     SELECT set_config('app.change_set_id', '<CS1>', true);
     UPDATE lineage_nodes SET attrs = '{}' WHERE id = '<S>';
     ROLLBACK;
     ```
  5. Create a change set with a blank reason:
     ```sql
     INSERT INTO change_sets (tenant_id, title, reason, author, state, items)
       VALUES ('00000000-0000-4000-8000-000000000001', 't', '  ', 'uat', 'proposed', '[]');
     ```
  6. Rewrite and erase history:
     ```sql
     UPDATE record_revisions SET reason = 'edited';
     DELETE FROM record_revisions;
     TRUNCATE record_revisions;
     ```
  7. Try to reopen a decided change set and delete change sets:
     ```sql
     UPDATE change_sets SET state = 'proposed' WHERE id = '<CS1>';
     DELETE FROM change_sets;
     ```
  8. Back in the terminal, check that a run state change still works without a change set: `as alice POST /v1/runs/$RUNB/transitions '{"to":"running"}'`. Leave the shell with `\q`.
- **Expected:**
  - Steps 2, 3 and 4: each fails with an error carrying the code `LDE03` ("changed outside a change set committed in this transaction"). The attrs are unchanged.
  - Step 5: fails with a check violation, code `23514` (if a different not-null error appears first, ask the developer to adjust the statement; the blank reason must still be refused).
  - Step 6: each of the three fails with code `LDE03`, even as the database owner.
  - Step 7: both fail with `LDE03`.
  - Step 8: `[200]` and the run is `running`.
- **Result:** Pass / Fail · Notes:

### UAT-14: Every change is audited and the chain still verifies

- **Requirements:** SPEC-007-R01, SPEC-007-R04, SPEC-007-R11
- **As:** QA / validation staff
- **Steps:**
  1. In the browser open **Audit trail** and click **Verify chain**.
  2. From the terminal: `as alice GET /v1/audit/verify`.
  3. List the events from this session:
     `events | jq -r '.[] | select(.action|test("^(change_set|record|note)\\.")) | [.seq,.actor,.action,.reason] | @tsv'`
  4. Compare with what you did: the direct edits (UAT-01 to UAT-04), the proposals, commit, rejection and withdrawals (UAT-06 to UAT-10), and the notes (UAT-11, UAT-12).
  5. Look for any event for a refused request, such as the agent's commit (UAT-07) or the sealed-run edits (UAT-11).
- **Expected:**
  - Steps 1 and 2: "Chain valid", with matching engine and database heads.
  - Step 3 and 4: `change_set.committed` and `record.revised` for every direct edit (actor, reason as typed), `change_set.proposed` for every proposal (the agent's shows actor `alice via bot`), `change_set.updated` (UAT-09 step 3), `change_set.committed` for the inbox commit, `change_set.rejected` (actor `bob`, reason `use 40 cycles`), `change_set.withdrawn`, and `note.created` for each note (payload has `annotation` true only for the note on the signed run). Revisions after the first have a `record.revised` event; first revisions have none.
  - Step 5: refused attempts (403 and 409 responses) wrote no event. The failed commit in UAT-10 left no `change_set.committed` event.
- **Result:** Pass / Fail · Notes:

## 3. Traceability

| Requirement | Cases | Automated test |
|---|---|---|
| SPEC-007-R01 | UAT-01, UAT-02, UAT-03, UAT-04, UAT-12, UAT-14 | yes |
| SPEC-007-R02 | UAT-01 (blank reason refused), UAT-13 | yes |
| SPEC-007-R03 | UAT-13 | yes |
| SPEC-007-R04 | UAT-10, UAT-14 | yes |
| SPEC-007-R05 | UAT-06, UAT-08 | yes |
| SPEC-007-R06 | UAT-07 | yes |
| SPEC-007-R07 | UAT-05 | — |
| SPEC-007-R08 | UAT-05 | — |
| SPEC-007-R09 | UAT-05 (blame and tooltips), UAT-02, UAT-03, UAT-04 (changed markers) | — |
| SPEC-007-R10 | UAT-11 | yes |
| SPEC-007-R11 | UAT-01, UAT-02, UAT-03, UAT-04, UAT-14 | — |
| SPEC-007-R12 | UAT-08, UAT-09 | — |
| SPEC-007-R13 | UAT-11, UAT-12 | yes |

Out of scope and not tested here, per the spec: committing changes to sealed records and the correction and re-signing flow (SPEC-008), revisions for audit events, signatures, protocol versions and readings, branching or merging lab data, and real-time collaborative editing. A service (parser) principal is not exercised separately: the agent case in UAT-07 covers the same database rule. The `reviewer` versus `scientist` role split cannot be shown on a development server, where every new handle holds every role.

## 4. Sign-off

| Tester | Role | Date | Outcome | Notes |
|---|---|---|---|---|
| | | | Accepted · Accepted with issues · Rejected | |
