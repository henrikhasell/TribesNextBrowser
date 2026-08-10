// Package dbproxy answers the stored-procedure ordinals Tribes 2's community
// scripts issue.
//
// Every pane of the shell's Community section reaches its backend through one
// of two calls:
//
//	DatabaseQuery(ordinal, args, proxyObject, key)                webstuff.cs:218
//	DatabaseQueryArray(ordinal, maxRows, args, proxyObject, key)          :223
//
// The ordinals are opaque numbers. They were Oracle stored-procedure selectors
// -- the client tests for ORA-04061 by name in seven separate handlers -- so
// the only way to name one is to read the call site that issues it and the
// handler that consumes the answer. That is what the table below is, and every
// entry cites both.
//
// Read Row as a statement about the client and nothing more. Field 9 of an
// email row is labelled created because webemail.cs:1121 assigns
// getField(%row,9) to %created; that is a fact. What WON's own server put there
// is not recoverable and is not claimed. Fields no parser reads are "-" and are
// padding: they must exist so the indices line up, and their contents are
// arbitrary.
//
// The form is part of the key, not decoration. On the wire DatabaseQuery(1,..)
// sent "1 0" and DatabaseQueryArray(1,..) sent "C1 <maxRows>", and four
// ordinals collide across the two -- 0, 1, 10 and 11 -- with unrelated meanings
// every time: scalar 10 is addBuddy, array 10 is a tribe roster. Keying on the
// ordinal alone would silently merge them.
//
// The table is derived from ../won-test-harness/wonproxy/ordinals.py, which
// established it by driving the shipped binary against a stand-in and recording
// both directions. Line numbers refer to the scripts as they come out of
// base/scripts.vl2.
package dbproxy

// Form distinguishes the two call shapes, which select different procedures.
const (
	Scalar = "scalar"
	Array  = "array"
)

// Key is what a request selects. Both halves matter; see the package comment.
type Key struct {
	Form    string
	Ordinal string
}

// Ordinal is one entry of the table: what the client calls, where from, and
// what it does with the answer.
type Ordinal struct {
	Name string
	Pane string
	// Where is the call site, <script>:<line>.
	Where string
	// Args is the argument tuple as the call site assembles it.
	Args string
	// ParsedBy is the handler that pins the row schema. Empty for the
	// fire-and-forget writes whose only observable is the status.
	ParsedBy string
	// Row is the field layout, as read off ParsedBy. Empty when no rows are
	// read.
	Row string
	// Result is what the handler takes out of the resultString.
	Result string
	// Note records a decision, an ambiguity or a defect in the shipped client.
	Note string
}

func k(form, ordinal string) Key { return Key{Form: form, Ordinal: ordinal} }

// Table is every (form, ordinal) pair reachable from the five community
// scripts. All 61 are present and all 61 have a handler; the test in
// dbproxy_test.go asserts that, because an ordinal in the table with nothing
// behind it would answer a refusal that reads like a server fault.
var Table = map[Key]Ordinal{

	// ---------------------------------------------------------------- news --

	k(Scalar, "0"): {
		Name: "getMOTD", Pane: "news",
		Where:    "webnews.cs:77",
		Args:     "(none)",
		ParsedBy: "webnews.cs:481 NewsMOTDText::onDatabaseQueryResult",
		Result:   "the message of the day, verbatim",
		Note: "The handler does setText(%RowCount_Result): for this ordinal " +
			"the resultString IS the payload, not a row count.",
	},
	k(Scalar, "1"): {
		Name: "postNewsArticle", Pane: "news",
		Where: "webnews.cs:356",
		Args:  "<category> \\t <title> \\t <body>",
	},
	k(Scalar, "2"): {
		Name: "editNewsArticle", Pane: "news",
		Where: "webnews.cs:362",
		Args:  "<editUrl> \\t <category> \\t <title> \\t <body>",
	},
	k(Scalar, "3"): {
		Name: "deleteNewsArticle", Pane: "news",
		Where: "webnews.cs:449",
		Args:  "<fields, assembled by the caller>",
	},
	k(Scalar, "4"): {
		Name: "setMOTD", Pane: "news",
		Where: "webnews.cs:524",
		Args:  "<motd text>",
	},
	k(Array, "0"): {
		Name: "getNewsArticles", Pane: "news",
		Where:    "webnews.cs:467, :478",
		Args:     "\"1\" \\t <categoryId>",
		ParsedBy: "webnews.cs:244 NewsGui::onDatabaseRow",
		Row: "0=- 1=topicId 2=articleId 3=postCount+1 4=date 5=updateId " +
			"6=authorId 7=- 8..11=authorQuad 12=category 13=headline 14..=body lines",
		Result: "field 0 = row count; status field 2 = total records, field 3 = acl",
		Note: "maxRows carries $currentPage, not a row limit -- the caller " +
			"passes the page number in that slot (webnews.cs:467).",
	},
	k(Array, "100"): {
		Name: "getNewsByCategory", Pane: "news",
		Where:    "webnews.cs:612",
		Args:     "\"0\" \\t \"0\" \\t <categoryTabId>",
		ParsedBy: "webnews.cs:244 NewsGui::onDatabaseRow",
		Row:      "same as array 0 -- both feed NewsGui::rebuildText",
		Result:   "field 0 = row count; status field 2 = total, field 3 = acl",
		Note: "Category ids are the tab ids: 105 FRONT PAGE, 35500 EVENTS, " +
			"35501 SPORTS, 35503 TECH, 35504 EDITORIAL (webnews.cs:62-66).",
	},

	// ------------------------------------------------------------ weblinks --

	k(Array, "15"): {
		Name: "getWebLinks", Pane: "weblinks",
		Where:    "webnews.cs:82",
		Args:     "\"WEBLINK\"",
		ParsedBy: "weblinks.cs:86 weblinksmenu::onDatabaseRow",
		Row:      "0=status (\"0\" to accept the row) 1=name 2=address",
		Result:   "(only status field 0 is tested)",
		Note: "On any non-zero status the client falls back to " +
			"weblinksmenu::defaultList -- 50 hardcoded fan sites " +
			"(weblinks.cs:1-56), the only surviving record of what this " +
			"pane served.",
	},

	// --------------------------------------------------------------- email --

	k(Array, "1"): {
		Name: "getMail", Pane: "email",
		Where:    "webemail.cs:387 (CheckEmail)",
		Args:     "$EmailNextSeq -- the highest message id already cached",
		ParsedBy: "webemail.cs:1109 EMailGui::onDatabaseRow, state \"NewMail\"",
		Row: "0=id 1..4=senderQuad 5..8=recipientQuad 9=created 10=isCC " +
			"11=isDeleted 12=isRead 13=toList 14=ccList 15=subject " +
			"16=bodyLineCount 17..=body lines",
		Result: "field 0 = number of new messages; 0 means nothing new and the " +
			"client reschedules a poll in 5 minutes",
		Note: "The argument is a high-water mark and a server MUST filter on " +
			"it. Ignoring it makes the inbox grow without bound: the pane " +
			"caches what it is handed and polls again with the highest id it " +
			"has, so unfiltered replies list every message once per poll.",
	},
	k(Array, "2"): {
		Name: "getBlockList", Pane: "email",
		Where:    "webemail.cs:407 (EmailEditBlocks)",
		Args:     "(none)",
		ParsedBy: "webemail.cs:555 EMailBlockDlg::onDatabaseRow, state \"names\"",
		Row:      "0..3=nameQuad (0=name 1=tag 2=append 3=uid) 4=blockedCount",
		Result:   "field 0 = number of blocked senders",
		Note: "getTextName(getFields(%row,0,4)) consumes fields 0..4 but " +
			"getTextName itself (webstuff.cs:19) reads only four; field 4 is " +
			"then re-read as the block count.",
	},
	k(Array, "14"): {
		Name: "getDeletedMail", Pane: "email",
		Where:    "webemail.cs:1029",
		Args:     "EmailGui.state",
		ParsedBy: "webemail.cs:1109 EMailGui::onDatabaseRow, state \"DeletedMail\"",
		Row:      "identical to array 1",
		Result:   "field 0 = row count; 0 shows status field 2 in a MessageBoxOK",
	},
	k(Scalar, "5"): {
		Name: "sendMail", Pane: "email",
		Where: "webemail.cs:155 (EmailSend)",
		Args: "<to> \\t <cc> \\t <subject> \\t " +
			"<body, truncated to 4000 - len(to+cc+subject)>",
		ParsedBy: "webemail.cs:478, state \"sendMail\"",
		Note: "The 4000-character budget is shared across all four fields " +
			"(webemail.cs:154) -- evidence the backend column was 4000 wide.",
	},
	k(Scalar, "6"): {
		Name: "deleteMail", Pane: "email",
		Where:    "webemail.cs:141 via DoEmailDelete, %qnx = 6",
		Args:     "<messageId>",
		ParsedBy: "webemail.cs:478, state \"deleteMail\"",
		Note:     "6 moves to the deleted folder; 35 removes permanently.",
	},
	k(Scalar, "7"): {
		Name: "markMailRead", Pane: "email",
		Where:    "webemail.cs:1328 (EM_Browser::onSelect)",
		Args:     "<messageId>",
		ParsedBy: "(none -- the call passes no proxy object)",
		Result:   "(discarded)",
		Note: "The only call site in the five scripts that omits both the " +
			"proxy object and the key: DatabaseQuery(7, %id). The answer is " +
			"reassembled and thrown away. Fire-and-forget by construction.",
	},
	k(Scalar, "8"): {
		Name: "removeBlock", Pane: "email",
		Where:    "webemail.cs:445",
		Args:     "<name>",
		ParsedBy: "webemail.cs:525, state \"removeBlock\"",
		Result:   "status field 1 is shown in a MessageBoxOK",
	},
	k(Scalar, "9"): {
		Name: "addBlock", Pane: "email",
		Where:    "webemail.cs:426, webbrowser.cs:375",
		Args:     "<blockAddress>",
		ParsedBy: "webemail.cs:525, state \"setBlock\"",
		Result:   "status field 1 is shown in a MessageBoxOK",
	},
	k(Scalar, "35"): {
		Name: "removeMailPermanently", Pane: "email",
		Where:    "webemail.cs:141 via DoEmailDelete, %qnx = 35",
		Args:     "<messageId>",
		ParsedBy: "webemail.cs:478, state \"removeMail\"",
	},

	// ------------------------------------------------------- email/browser --

	k(Array, "3"): {
		Name: "searchWarriors", Pane: "email/browser",
		Where: "webemail.cs:830 (AddressDlg), webbrowser.cs:95 with " +
			"BrowserSearchPane.query == 3",
		Args: "<search text> \\t <start> \\t <count> \\t <flag>",
		ParsedBy: "webemail.cs:725 AddressDlg (reads field 1); " +
			"webbrowser.cs:932 BrowserSearchPane (reads fields 1..)",
		Row:    "0=- 1=name 2..=further columns the browser shows",
		Result: "field 0 = match count; <= 0 shows \"No Players found\"",
		Note: "The trailing flag differs by caller -- the address book sends " +
			"1, the browser 0. What it selects is not recoverable.",
	},
	k(Array, "5"): {
		Name: "getBuddyList", Pane: "email/browser",
		Where: "webemail.cs:850 (AddressDlg), webbrowser.cs:1940 " +
			"(PlayerPane::ButtonClick case 3, BUDDYLIST)",
		Args:     "(none) from the address book; <playerName> from the browser",
		ParsedBy: "webemail.cs:725 AddressDlg; webbrowser.cs:1841 PlayerPane",
		Row:      "0..3=buddyQuad 4=friendsSince 5=online",
		Result:   "field 0 = count; <= 0 shows \"You have no Buddies.\"",
		Note: "Field 5 feeds a setRowStyleById that is commented out at " +
			"webbrowser.cs:1847, so the online flag is carried but unused.",
	},
	k(Array, "6"): {
		Name: "getTribeMembers", Pane: "email/browser",
		Where: "webemail.cs:857 (AddressDlg), webbrowser.cs:1583 " +
			"(TribePane::ButtonClick case 1, ROSTER)",
		Args:     "<tribe name>",
		ParsedBy: "webemail.cs:725 AddressDlg; webbrowser.cs:1470 TribePane",
		Row: "0..3=memberQuad 4=title 5=adminLevel 6=joinDate 7=- " +
			"8=canEditKick 9=online",
		Result: "field 0 = count",
		Note: "The ROSTER button uses this ordinal, not array 10 -- the three " +
			"MemberList columns it adds beforehand (MEMBER / TITLE / RNK, " +
			"webbrowser.cs:1580-1582) line up with fields 0, 4 and 5.",
	},
	k(Scalar, "10"): {
		Name: "addBuddy", Pane: "email/browser",
		Where:    "webemail.cs:764, webbrowser.cs:367",
		Args:     "<playerName>",
		ParsedBy: "webemail.cs:658, state \"addBuddy\"",
		Result:   "status field 1 goes in a MessageBoxOK",
	},
	k(Scalar, "11"): {
		Name: "dropBuddy", Pane: "email/browser",
		Where:    "webemail.cs:769, webbrowser.cs:359",
		Args:     "<playerName>",
		ParsedBy: "webemail.cs:658, state \"dropBuddy\"",
		Result:   "status field 1 goes in a MessageBoxOK",
	},
	k(Scalar, "69"): {
		Name: "getOnlineStatus", Pane: "email/browser",
		Where:    "webemail.cs:655, webbrowser.cs:866, :2041, :2223",
		Args:     "<tab-separated list of row ids>",
		ParsedBy: "webbrowser.cs:879 BrowserSearchPane, state \"getOnline\"",
		Result:   "one character per queried id: setRowStyle(i, !char)",
		Note: "A fixed-width bitmap indexed by character position, not a " +
			"field list. The handler also reads an undefined %resultString " +
			"(the parameter is named %resultStatus), so the loop never runs -- " +
			"a defect in the shipped script. Answered correctly anyway.",
	},

	// ------------------------------------------------------------- browser --

	k(Array, "4"): {
		Name: "searchTribes", Pane: "browser",
		Where:    "webbrowser.cs:95 with BrowserSearchPane.query == 4",
		Args:     "<search text> \\t <start> \\t <count> \\t 0",
		ParsedBy: "webbrowser.cs:932 BrowserSearchPane, state \"tribe\"",
		Row:      "0=- 1=tribeName 2=tribeTag 3..=further columns",
		Result:   "field 0 = match count; <= 0 shows \"No Tribes found\"",
		Note: "getTribeName (webbrowser.cs:316) formats fields 1 and 2 as " +
			"\"<name> - <tag>\" and keeps field 1 as the tab name.",
	},
	k(Array, "10"): {
		Name: "getTribeNews", Pane: "browser",
		Where:    "webbrowser.cs:1588 (TribePane::ButtonClick case 2, NEWS)",
		Args:     "<tribeName>",
		ParsedBy: "webbrowser.cs:1506 TribePane state \"tribeNews\" -- DEAD CODE",
		Row: "0=articleId 1=forumName 2..4=- 5..8=authorQuad 9=lastUpdated " +
			"10=title 11..=body lines",
		Result: "field 0 = article count",
		Note: "Cut before release. The handler sets state = \"done\" " +
			"unconditionally (webbrowser.cs:1410) and the block that would " +
			"have set \"tribeNews\" is commented out immediately below " +
			"(:1412-1420), so the row branch is unreachable and any rows " +
			"served are discarded. The pane shows a static list of Tribal " +
			"Forum / Tribal Chat links instead (:1399-1407). The schema above " +
			"is read off the dead branch and is the only surviving " +
			"description of what this ordinal returned.",
	},
	k(Array, "11"): {
		Name: "getTribeInvites", Pane: "browser",
		Where:    "webbrowser.cs:1599 (TribePane::ButtonClick case 3, INVITE)",
		Args:     "<tribeName>",
		ParsedBy: "webbrowser.cs:1518 TribePane state \"tribeInvites\"",
		Row: "0=inviteId 1=inviteDate 2..5=invitorQuad 6..9=invitedQuad " +
			"10=isOwned 11=online",
		Result: "field 0 = invite count",
		Note: "Field 11 drives MemberList.setRowStyleById(id, !online) at " +
			"webbrowser.cs:1528, so the flag is read in that inverted sense.",
	},
	k(Array, "12"): {
		Name: "getWarriorHistory", Pane: "browser",
		Where:    "webbrowser.cs:1897 (PlayerPane::ButtonClick case 1, HISTORY)",
		Args:     "<playerName>",
		ParsedBy: "webbrowser.cs:1820 PlayerPane state \"warriorHistory\"",
		Row:      "the whole row is one line of display text -- no fields",
		Result:   "field 0 = line count",
		Note: "The only ordinal whose rows are not field-structured. Rows are " +
			"appended verbatim to a GuiMLTextCtrl, so they may carry Torque " +
			"markup.",
	},
	k(Array, "13"): {
		Name: "getWarriorTribeList", Pane: "browser",
		Where:    "webbrowser.cs:1929 (PlayerPane::ButtonClick case 2, TRIBES)",
		Args:     "<playerName>",
		ParsedBy: "webbrowser.cs:1831 PlayerPane state \"warriorTribeList\"",
		Row:      "0=tribeName 1=- 2=tribeId 3=adminLevel 4=editKick 5=title",
		Result:   "field 0 = tribe count",
		Note: "Only issued for OTHER players. Viewing your own tribe list " +
			"takes the fields straight out of WONGetAuthInfo() and sends no " +
			"query at all (webbrowser.cs:1909-1927) -- which is also where " +
			"the field order is corroborated, since that loop fills the same " +
			"columns from the certificate.",
	},
	k(Scalar, "15"): {
		Name: "setTribeDescription", Pane: "browser",
		Where: "webbrowser.cs:208, :2436",
		Args:  "<tribeName> \\t <lineCount> \\t <description>",
	},
	k(Scalar, "16"): {
		Name: "createTribe", Pane: "browser",
		Where:    "webbrowser.cs:166",
		Args:     "<name> \\t <tag> \\t <append>",
		ParsedBy: "webbrowser.cs:761 CreateTribeDlg::onDatabaseQueryResult",
	},
	k(Scalar, "17"): {
		Name: "setWarriorDescription", Pane: "browser",
		Where:    "webbrowser.cs:215 (EditDescriptionApply), :2553 (doClearDescription)",
		Args:     "<description text>, or the literal NONE to clear it",
		ParsedBy: "webbrowser.cs:1223 TWBText::onDatabaseQueryResult",
		Note: "Both call sites are the WARRIOR description, not the tribe " +
			"one -- :215 is the else branch of a test on TWBText.editType, " +
			"whose tribe branch sends ordinal 15 instead.",
	},
	k(Scalar, "18"): {
		Name: "deleteTribe", Pane: "browser",
		Where: "webbrowser.cs:343",
		Args:  "<tribeName>",
	},
	k(Scalar, "19"): {
		Name: "kickMember", Pane: "browser",
		Where: "webbrowser.cs:499 (LinkKickMember), state \"kickPlayer\"",
		Args:  "<player> \\t <tribe> \\t 0",
		Note: "Removes a member; it is not the invite ordinal, which is 27. " +
			"Only the helper says so -- the DatabaseQuery line alone does not.",
	},
	k(Scalar, "20"): {
		Name: "toggleTribeFlag", Pane: "browser",
		Where: "webbrowser.cs:520 (LinkTribeToggle)",
		Args:  "<\"Recruiting\"|\"Appending\"> \\t <tribeName> \\t <0|1>",
		Note: "Those two words are the only ones any of its four call sites " +
			"passes (webbrowser.cs:1120, :1122, :2386, :2395).",
	},
	k(Scalar, "21"): {
		Name: "setMemberProfile", Pane: "browser",
		Where:    "webbrowser.cs:643",
		Args:     "<tribe> \\t <player> \\t <title> \\t <adminLevel>",
		ParsedBy: "webbrowser.cs:805 TribeAdminMemberDlg",
	},
	k(Scalar, "22"): {
		Name: "getTribeProfile", Pane: "browser",
		Where:    "webbrowser.cs:1571 (TribePane::ButtonClick case 0, PROFILE)",
		Args:     "<tribeName>",
		ParsedBy: "webbrowser.cs:1353 TribePane, via GetProfileHdr(0, getFields(%status,2))",
		Result:   "the tribe description (TProfileHdr.Desc, webbrowser.cs:1362)",
		Note: "Carries its payload in the STATUS, not in rows: 0 code, " +
			"1 message, 2 tribeId, 3 tribeName, 4 tribeTag, 5 appending, " +
			"6 recruiting, 7 tribeGfx. Field 6 drives the \"Recruiting: " +
			"YES/NO\" line and the Request Invite link (:1358).",
	},
	k(Scalar, "23"): {
		Name: "getWarriorProfile", Pane: "browser",
		Where:    "webbrowser.cs:1890 (PROFILE) and :1947 (OPTIONS)",
		Args:     "<playerName>",
		ParsedBy: "webbrowser.cs:1663 PlayerPane, via GetProfileHdr(1, getFields(%status,2))",
		Result:   "the warrior description, appended to the profile markup (:1682)",
		Note: "Status layout: 0 code, 1 message, 2 playerName, 3 playerTag, " +
			"4 appending, 5 playerId, 6 registered, 7 online, 8 playerURL, " +
			"9 playerGfx. AMBIGUOUS at field 9: the same ordinal serves state " +
			"\"getVisitorOptions\", which reads it as a tribe COUNT with " +
			"fields 10.. a four-per-tribe list (:1745). Nothing on the wire " +
			"distinguishes them. Answered with the graphic, which both " +
			"readings tolerate -- Torque coerces a bitmap path to 0, so the " +
			"count fails its > 0 test and the options pane lists the caller's " +
			"tribes out of WONGetAuthInfo instead (:1762), the branch it takes " +
			"anyway. Sending it empty is NOT the neutral choice: $PlayerGfx, " +
			"the fallback onWake uses (:1646), is assigned by no shipped " +
			"script, so an empty field renders a permanently blank picture. " +
			"A graphic beginning with a digit WOULD read as a count, so it is " +
			"replaced by the default rather than sent.",
	},
	k(Scalar, "24"): {
		Name: "leaveTribe", Pane: "browser",
		Where:    "webbrowser.cs:497 (LinkLeaveTribe)",
		Args:     "<tribeName>",
		ParsedBy: "webbrowser.cs:1734 PlayerPane state \"leaveTribe\"",
		Note: "On success the client calls WonUpdateCertificate() and removes " +
			"the tribe's tab (webbrowser.cs:1736-1741).",
	},
	k(Scalar, "25"): {
		Name: "setPrimaryTribe", Pane: "browser",
		Where:    "webbrowser.cs:515 (LinkMakePrimary)",
		Args:     "<tribeId or tribeName>",
		ParsedBy: "webbrowser.cs:1714 PlayerPane, states set/setNoPrimaryTribe",
		Result: "field 0 = the tribe name, echoed into " +
			"\"<name> has been flagged as your primary tribe.\" (:1719)",
		Note: "One ordinal serves both set and clear; which branch runs is " +
			"decided entirely client-side by the state the caller set.",
	},
	k(Scalar, "26"): {
		Name: "clearBuddy", Pane: "browser",
		Where: "webbrowser.cs:351",
		Args:  "\"clearBuddy\"",
		Note:  "The literal string is the argument, not the ordinal's name.",
	},
	k(Scalar, "27"): {
		Name: "inviteToTribe", Pane: "browser",
		Where: "webbrowser.cs:527 (LinkInvitePlayer)",
		Args:  "<tribe> \\t <player>",
		Note: "This is the invite ordinal; 19 is the kick. Recording the " +
			"invitation does not make it reachable: no client query lists a " +
			"player's own invitations (array 11 is tribe-scoped and its tab " +
			"is admin-only, :247), so the invited warrior is MAILED a body " +
			"carrying acceptinvite/rejectinvite links. That is also what " +
			"PlayerPane expects -- it deletes the open message and re-checks " +
			"mail once the answer lands (:1726-1733).",
	},
	k(Scalar, "28"): {
		Name: "answerInvitation", Pane: "browser",
		Where:    "webbrowser.cs:564 (LinkInvitation)",
		Args:     "<\"accept\"|\"reject\"|\"cancel\"> \\t <tribe> \\t <player>",
		ParsedBy: "webbrowser.cs:1714 PlayerPane",
		Result:   "field 0 = the tribe name on accept (:1729)",
		Note: "\"cancel\" is the tribe withdrawing; the other two are the " +
			"invited player answering. AMBIGUOUS in the same way as 23: " +
			"accept and reject are also the only verbs onURL has for an admin " +
			"answering a JOIN REQUEST (ordinal 34), and the wire is identical. " +
			"Told apart by who is asking -- field 2 naming somebody other " +
			"than the caller, with the caller able to invite, is the admin " +
			"reading.",
	},
	k(Scalar, "29"): {
		Name: "setTribeGraphic", Pane: "browser",
		Where: "webbrowser.cs:2494",
		Args:  "<tribeName> \\t <bitmap>",
	},
	k(Scalar, "30"): {
		Name: "setTribeTag", Pane: "browser",
		Where: "webbrowser.cs:2413",
		Args:  "<tribeId> \\t <newTag>",
	},
	k(Scalar, "31"): {
		Name: "setPlayerGraphic", Pane: "browser",
		Where:    "webbrowser.cs:2591",
		Args:     "<bitmap>",
		ParsedBy: "webbrowser.cs:1654 PlayerPane",
		Note: "A path into the client's own texture volume, picked from a list " +
			"the dialog builds by scanning texticons/twb/twb_*.jpg (:2567) -- " +
			"nothing is uploaded. What makes the choice stick across a refresh " +
			"is field 9 of ordinal 23 coming back with it.",
	},
	k(Scalar, "32"): {
		Name: "setPlayerUrl", Pane: "browser",
		Where: "webbrowser.cs:2614",
		Args:  "<url>",
	},
	k(Scalar, "33"): {
		Name: "setPlayerName", Pane: "browser",
		Where: "webbrowser.cs:2628",
		Args:  "<newName>",
		Note: "Refused here. The account name belongs to the TribesNext " +
			"account: this server only caches the name it learns while " +
			"verifying a session and refreshes it on every request, so a local " +
			"change would be undone within the minute. Refusing in a sentence " +
			"beats succeeding and silently reverting.",
	},
	k(Scalar, "34"): {
		Name: "requestInvite", Pane: "browser",
		Where: "webbrowser.cs:1140 (GuiMLTextCtrl::onURL case \"requestlink\"), " +
			"reached from the Request Invite link a recruiting tribe draws (:1359)",
		Args:     "<tribeName>",
		ParsedBy: "webbrowser.cs:1445 TribePane state \"requestInvite\"",
		Note: "Status field 1 is user-visible -- :1446 puts it straight into a " +
			"MessageBoxOK -- so every branch must be a sentence, not a code. " +
			"Answered by recording the request and mailing the tribe's admins " +
			"a body carrying acceptinvite/rejectinvite links, which is the " +
			"only way it can be answered at all. NOT IDEMPOTENT: a second " +
			"request to the same tribe inside seven days is refused.",
	},
	k(Scalar, "63"): {
		Name: "postAdminAction", Pane: "browser",
		Where: "webbrowser.cs:1183-1211 (eight sites, first field 0..7)",
		Args:  "<action 0..7> \\t <fields 1.. of the parsed URL>",
		Note: "WON kept its staff in one tribe: webbrowser.cs:409 and :2248 " +
			"grant cross-tribe moderator powers to anyone whose certificate " +
			"contains tribe id 1401 with an admin level above 1. That is the " +
			"gate applied here. The eight-way selector is named nowhere in the " +
			"shipped scripts, so the action is recorded, not interpreted.",
	},

	// -------------------------------------------------------------- forums --

	k(Array, "7"): {
		Name: "getForumList", Pane: "forums",
		Where:    "webforums.cs:689",
		Args:     "(none)",
		ParsedBy: "webforums.cs:808 ForumsGui, state \"ForumList\"",
		Row:      "0=forumRowId 1=forumName 2=flag 3=forumId",
		Result:   "field 0 = forum count",
		Note: "After the last row the client appends one row per tribe from " +
			"WONGetAuthInfo() with a negated id, so tribe forums never came " +
			"from the server (webforums.cs:817-822).",
	},
	k(Array, "8"): {
		Name: "getTopicList", Pane: "forums",
		Where:    "webforums.cs:602, :920, :935",
		Args:     "<forumId>",
		ParsedBy: "webforums.cs:808 ForumsGui, state \"TopicList\"",
		Row: "0=- 1=topicId 2=topic 3=postCount 4=- 5=- 6=date 7=- " +
			"8=authorName 9=- 10=- 11=- 12=hasDeletes 13=securityLevel " +
			"14=maxUpdateId",
		Result: "field 0 = topic count",
		Note: "maxRows carries $currentForumPage at :602 and :920 but a real " +
			"limit (80) at :935 -- the slot is overloaded.",
	},
	k(Array, "9"): {
		Name: "getPostUpdates", Pane: "forums",
		Where:    "webforums.cs:627",
		Args:     "<topicId> \\t <lastPostIdSeen>",
		ParsedBy: "webforums.cs:808 ForumsGui, state \"PostList\"",
		Row: "0=isAuthor 1=- 2=postId 3=parentPostId 4=updateId 5..8=posterQuad " +
			"9=- 10=date 11=- 12=isDeleted 13=subject 14..=body lines",
		Result: "field 0 = post count; status field 2 = the per-forum flag the " +
			"client caches as ForumsGui.bflag",
		Note: "A post that is its own parent is a thread root and is " +
			"normalised to 0 (webforums.cs:876).",
	},
	k(Scalar, "12"): {
		Name: "postTopicOrReply", Pane: "forums",
		Where: "webforums.cs:527 with %ord = 12 (:482 new topic, :510 reply)",
		Args:  "<forumId> \\t <topicId> \\t <parentPost> \\t <subject> \\t <body>",
	},
	k(Scalar, "13"): {
		Name: "editPost", Pane: "forums",
		Where: "webforums.cs:527 with %ord = 13 (:518)",
		Args:  "<postId> \\t <subject> \\t <body>",
	},
	k(Scalar, "14"): {
		Name: "postNewsOrDeletePost", Pane: "forums",
		Where: "webforums.cs:527 with %ord = 14 (:494), :552, :1474",
		Args: "AMBIGUOUS: 3 fields <post> \\t <subject> \\t <body> at :494; " +
			"1 field <postId> at :552 and :1474",
		Note: "Three call sites, two argument shapes, three labels. Either the " +
			"procedure was overloaded on argument count or one is a " +
			"copy-paste error in the shipped script -- immediately below the " +
			"third, :1475 has the correct-looking call commented out in " +
			"favour of 14. The client cannot distinguish the cases, so this " +
			"handler branches on the field count and says so.",
	},
	k(Scalar, "60"): {
		Name: "requestTopicReview", Pane: "forums",
		Where: "webforums.cs:1185",
		Args:  "<forumId> \\t <topicId> \\t <field 11 of the topic row>",
	},
	k(Scalar, "61"): {
		Name: "requestPostReview", Pane: "forums",
		Where: "webforums.cs:1470 via PostsPopupMenu::AdminCall(61, ...)",
		Args:  "<forumId> \\t <topicId> \\t <postId> \\t <authorId>",
	},
	k(Scalar, "62"): {
		Name: "removeTopic", Pane: "forums/browser",
		Where: "webforums.cs:1207, webbrowser.cs:1148-1178 (eight sites)",
		Args:  "<action 0..7> \\t <topicId> \\t <field 11 of the topic row>",
		Note: "The browser issues it with a leading selector 0..7; the forums " +
			"pane with a leading 0. The selector is named nowhere.",
	},
	k(Scalar, "66"): {
		Name: "lockTopic", Pane: "forums",
		Where: "webforums.cs:1227 (TopicsPopupMenu::ExecuteLock)",
		Args:  "<topicId> \\t <reason>",
	},
	k(Scalar, "67"): {
		Name: "unlockTopic", Pane: "forums",
		Where: "webforums.cs:1194",
		Args:  "<topicId>",
	},
	k(Scalar, "68"): {
		Name: "moveTopic", Pane: "forums",
		Where: "webforums.cs:1236 (TopicsPopupMenu::ExecuteMove)",
		Args:  "<topicId> \\t <destForumId> \\t <destForumName>",
	},
}
