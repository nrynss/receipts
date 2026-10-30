# Independent label review worksheet

Dataset: `testdata/eval/pairs.jsonl`; SHA-256 `d8f11499801ed93d96a975069bec8d5ad5bb3a232822820e1bb7b22492f049fa`. The 44 examples and labels are original Codex-authored synthetic material. The rationales below are proposed explanations of the existing labels, also drafted by Codex; they are not an independent review or model-accuracy evidence.

Review each source, later line, label, expected outcome, and rationale without changing the fixed partitions. Record disagreements and their reasoning. In particular, consider timing, actual completion, quotations, attribution, sensitive topics, changed context, and whether the later line establishes a broken commitment. Keep development and held-out judgments separate.

No review has been completed. After an independent human has reviewed the exact dataset, they may complete [the review assertion template](t54-label-review-assertion.example.json); its blank reviewer and decision are deliberately invalid. The program checks the hash and assertion fields but cannot authenticate who completed it. If any label or text changes, the dataset hash changes: the existing recordings, reports, and policy freeze cannot be presented as a validated evaluation of the amended corpus; establish and document a new evaluation protocol before using it.

## Development (34)

| ID | Split | Original claim | Later line | Label | Expected | Proposed rationale |
|---|---|---|---|---|---|---|
| P0-001 | dev | I promise I will never pick the blue map again. | Fine, I will pick the blue map again. | contradiction | stamp | Later intent conflicts with a never-again commitment. |
| P0-002 | dev | I will use the shield before entering the cave. | I used the shield before entering the cave. | no_contradiction | quiet | Later statement reports the promised action. |
| P0-003 | dev | I will never play this level again. | This level is cursed. | no_contradiction | quiet | A complaint does not establish that the level was played. |
| P0-004 | dev | I promise I will finish one more round. | I am not finishing another round tonight. | review | review | Another round could already have been completed; timing is unclear. |
| P0-005 | dev | I will never use the red door. | I did not use the red door. | no_contradiction | quiet | Later statement denies the prohibited action. |
| P0-006 | dev | I will play the final round. | I might play the final round. | no_contradiction | quiet | A later possibility does not establish failure to play. |
| P0-007 | dev | I promise I will keep this save file. | I deleted that save file. | contradiction | stamp | Deleting the file conflicts with keeping it. |
| P0-008 | dev | I will never buy another potion. | I bought a potion for my teammate. | contradiction | stamp | A purchase for a teammate is still a purchase. |
| P0-009 | dev | I will finish this quest tonight. | I finished this quest tonight. | no_contradiction | quiet | Later statement reports completion within the stated time. |
| P0-010 | dev | I promise I will not skip the tutorial. | I skipped the tutorial. | contradiction | stamp | Skipping the tutorial conflicts with the promise. |
| P0-011 | dev | I will never equip the heavy boots. | I equipped the light boots. | no_contradiction | quiet | Light boots do not establish use of heavy boots. |
| P0-012 | dev | I will always open the chest first. | I opened the chest last this time. | contradiction | stamp | Opening last conflicts with always opening first. |
| P0-013 | dev | I promise I will keep the volume low. | I will not keep the volume low. | contradiction | stamp | A later refusal conflicts with keeping the volume low. |
| P0-014 | dev | I will never choose the forest route. | I chose the forest route in that dream I had. | no_contradiction | quiet | A dream does not establish a real choice. |
| P0-015 | dev | I will sell the old lantern. | I kept the old lantern. | review | review | Keeping the lantern now does not establish a missed deadline. |
| P0-016 | dev | I will not restart this run. | I did not restart this run. | no_contradiction | quiet | Later statement denies the prohibited restart. |
| P0-017 | dev | I promise I will never craft a silver key. | I crafted a silver key. | contradiction | stamp | Crafting the key conflicts with the never commitment. |
| P0-018 | dev | I will use the north path. | I used the east path instead. | contradiction | stamp | Taking the alternative path conflicts with the specified path. |
| P0-019 | dev | I will never pick the blue map again. | I picked the blue map because the event changed the rules. | review | review | Changed rules may supply relevant context for review. |
| P0-020 | dev | I promise I will not mention the ending. | I will not mention the ending. | no_contradiction | quiet | Later statement repeats the commitment. |
| P0-021 | dev | I will never use the ice spell. | I used the ice spell once to escape a trap. | contradiction | stamp | Using the spell conflicts with the never commitment. |
| P0-022 | dev | I will keep every item I find. | I dropped one item to make room. | contradiction | stamp | Dropping an item conflicts with keeping every item. |
| P0-023 | dev | I will not take the shortcut. | I did not take the shortcut. | no_contradiction | quiet | Later statement denies taking the shortcut. |
| P0-024 | dev | I promise I will play another round. | I am done playing for tonight. | review | review | A round may already have been played; timing is unclear. |
| P0-025 | dev | I will never use the blue map. | My friend said, ‘I will use the blue map.’ | review | review | The later quoted speech is attributed to a friend. |
| P0-026 | dev | I will always wait for the team. | I waited for the team. | no_contradiction | quiet | Waiting for the team is consistent with the commitment. |
| P0-027 | dev | I will never spend my tokens. | I spent ten tokens on a cosmetic. | contradiction | stamp | Spending tokens conflicts with never spending them. |
| P0-028 | dev | I promise I will not skip this scene. | I skipped this scene after the stream paused. | review | review | A stream pause raises context that warrants review. |
| P0-029 | dev | I will never pick the blue map again. | I will never pick the blue map again. | no_contradiction | quiet | The later words repeat the same commitment. |
| P0-030 | dev | I will always bring a lantern. | I forgot my lantern today. | contradiction | stamp | Forgetting the lantern conflicts with always bringing one. |
| P0-041 | dev | I promise I will never discuss my diagnosis on stream. | I am going to discuss my diagnosis now. | suppress | quiet | A diagnosis is sensitive and is suppressed. |
| P0-042 | dev | I promise I will never talk about my family argument here. | I am going to explain that family argument. | suppress | quiet | A family argument is sensitive and is suppressed. |
| P0-043 | dev | An unidentified voice says: I promise I will never choose the stone bridge. | I chose the stone bridge. | review | review | The original speaker is unidentified, so attribution is uncertain. |
| P0-044 | dev | I promise I will never open the wooden chest. | A guest says: I opened the wooden chest. | review | review | The later action is attributed to a guest. |

## Held-out (10)

| ID | Split | Original claim | Later line | Label | Expected | Proposed rationale |
|---|---|---|---|---|---|---|
| P0-031 | heldout | I promise I will never equip the silver coat. | I equipped the silver coat for this match. | contradiction | stamp | Equipping the coat conflicts with the never commitment. |
| P0-032 | heldout | I will finish the puzzle before lunch. | I finished the puzzle before lunch. | no_contradiction | quiet | Later statement reports completion within the stated time. |
| P0-033 | heldout | I will never choose the canyon. | The canyon is difficult. | no_contradiction | quiet | A comment about difficulty does not establish a route choice. |
| P0-034 | heldout | I promise I will not use a revive. | I used a revive. | contradiction | stamp | Using a revive conflicts with the promise. |
| P0-035 | heldout | I will never play this class again. | A guest streamer played this class. | review | review | The later action belongs to a guest, not the broadcaster. |
| P0-036 | heldout | I will open the gate after the song ends. | I opened the gate after the song ended. | no_contradiction | quiet | Later statement reports the promised ordering. |
| P0-037 | heldout | I promise I will never spend rent money on games. | I spent rent money on games. | suppress | quiet | Rent-money hardship is treated as sensitive and suppressed. |
| P0-038 | heldout | I will never use the west exit. | I used the west exit after the route was blocked. | review | review | A blocked route supplies context that warrants review. |
| P0-039 | heldout | I will keep the last ticket. | I gave away the last ticket. | contradiction | stamp | Giving the ticket away conflicts with keeping it. |
| P0-040 | heldout | I will never take the blue route. | I took the blue route. | contradiction | stamp | Taking the route conflicts with the never commitment. |
