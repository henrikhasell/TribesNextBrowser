package api

import (
	"context"
	"strconv"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// browserMethods is the json_browser.php method table: the 26 documented
// TribesNext methods, plus the buddy and block lists the WON-era screens had
// and TribesNext dropped.
func (s *Server) browserMethods() map[string]handler {
	return map[string]handler{
		// -- reads --
		"usersearch": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.UserSearch(ctx, p.Q)
		},
		"userview": func(ctx context.Context, guid string, p payload) (any, error) {
			return notFoundIsEmpty(s.Store.UserView(ctx, p.ID))
		},
		"userhistory": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.UserHistory(ctx, p.ID)
		},
		"clansearch": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.ClanSearch(ctx, p.Q)
		},
		"clanview": func(ctx context.Context, guid string, p payload) (any, error) {
			return notFoundIsEmpty(s.Store.ClanView(ctx, p.id64()))
		},
		"clanhistory": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.ClanHistory(ctx, p.id64())
		},
		"userinvites": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.UserInvites(ctx, guid)
		},
		"clanviewinvites": func(ctx context.Context, guid string, p payload) (any, error) {
			list, err := s.Store.ClanInvites(ctx, guid, p.id64())
			if err != nil {
				return nil, err
			}
			// This one method wraps its list in a status envelope, unlike every
			// other reader. Preserved because the client unwraps "payload".
			return map[string]any{"status": "success", "payload": list}, nil
		},

		// -- own account --
		"userinfo": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetInfo(ctx, guid, p.Info)
		},
		"usersite": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetWebsite(ctx, guid, p.Site)
		},
		"userclan": func(ctx context.Context, guid string, p payload) (any, error) {
			id, err := strconv.ParseInt(p.ID, 10, 64)
			if err != nil {
				// The documented way to clear the tag is -1; anything
				// unparseable is treated the same rather than erroring.
				id = -1
			}
			return model.OK(), s.Store.SetActiveClan(ctx, guid, id)
		},
		"username": func(ctx context.Context, guid string, p payload) (any, error) {
			// Renames belong to TribesNext: the name here is a cache of theirs,
			// refreshed on every verified request, so changing it locally would
			// be undone within the minute. Refuse honestly rather than pretend.
			return model.Status{
				Status: "error",
				Msg:    "account names are managed by TribesNext and cannot be changed here",
			}, nil
		},
		"useraccept": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.AcceptInvite(ctx, guid, p.id64())
		},
		"userreject": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.RejectInvite(ctx, guid, p.id64())
		},
		"userleave": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.LeaveClan(ctx, guid, p.id64())
		},
		"createclan": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.CreateClan(ctx, guid, p.Name, p.Tag, truthy(p.Append))
		},

		// -- clan administration --
		"claninfo": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetClanInfo(ctx, guid, p.id64(), p.V)
		},
		"clansite": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetClanWebsite(ctx, guid, p.id64(), p.V)
		},
		"clanname": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetClanName(ctx, guid, p.id64(), p.V)
		},
		"clanpicture": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetClanPicture(ctx, guid, p.id64(), p.V)
		},
		"clanrecruit": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetClanRecruiting(ctx, guid, p.id64(), truthy(p.V))
		},
		"clantag": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.SetClanTag(ctx, guid, p.id64(), p.Tag, truthy(p.Append))
		},
		"claninvite": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.Invite(ctx, guid, p.id64(), p.To)
		},
		"clanrank": func(ctx context.Context, guid string, p payload) (any, error) {
			rank, err := strconv.Atoi(p.Rank)
			if err != nil {
				return model.Status{Status: "error", Msg: "rank must be an integer 0 to 4"}, nil
			}
			return model.OK(), s.Store.SetRank(ctx, guid, p.id64(), p.To, rank, p.Title)
		},
		"clankick": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.Kick(ctx, guid, p.id64(), p.To)
		},
		"clandisband": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.Disband(ctx, guid, p.id64(), truthy(p.V))
		},

		// -- beyond TribesNext: the WON-era social lists --
		"buddylist": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.BuddyList(ctx, guid)
		},
		"buddyadd": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.BuddyAdd(ctx, guid, p.To)
		},
		"buddyremove": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.BuddyRemove(ctx, guid, p.To)
		},
		"buddyclear": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.BuddyClear(ctx, guid)
		},
		"blocklist": func(ctx context.Context, guid string, p payload) (any, error) {
			return s.Store.BlockList(ctx, guid)
		},
		"blockadd": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.BlockAdd(ctx, guid, p.To)
		},
		"blockremove": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.BlockRemove(ctx, guid, p.To)
		},
	}
}
