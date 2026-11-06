package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Discord role mutation helpers.
//
// Role add/remove live in a per-guild bucket, so a bulk sync (200+ players) WILL
// brush against it. Instead of logging a 429 and dropping the change, every
// mutation is retried after the server-provided retry_after.

// errDiscordNotInGuild is returned when the target isn't a guild member.
var errDiscordNotInGuild = fmt.Errorf("discord member not in guild")

// discordRoleMutation performs a PUT/DELETE against a Discord endpoint, waiting
// out rate limits (up to 4 attempts).
func discordRoleMutation(method, url, botToken string) error {
	for attempt := 0; attempt < 4; attempt++ {
		req, _ := http.NewRequest(method, url, nil)
		req.Header.Set("Authorization", "Bot "+botToken)

		resp, err := discordRoleClient.Do(req)
		if err != nil {
			return err
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		switch {
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent:
			return nil
		case resp.StatusCode == http.StatusNotFound:
			return errDiscordNotInGuild
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := 600 * time.Millisecond
			var rl struct {
				RetryAfter float64 `json:"retry_after"`
			}
			if json.Unmarshal(body, &rl) == nil && rl.RetryAfter > 0 {
				wait = time.Duration(rl.RetryAfter*float64(time.Second)) + 100*time.Millisecond
			}
			if wait > 5*time.Second {
				wait = 5 * time.Second
			}
			time.Sleep(wait)
			continue
		default:
			return fmt.Errorf("%s failed %s %s", method, resp.Status, string(body))
		}
	}

	return fmt.Errorf("%s still rate limited after retries", method)
}

// discordGetMemberRoles fetches a member's role ids. Returns errDiscordNotInGuild
// when the user isn't in the guild.
func discordGetMemberRoles(guildID string, userID string, botToken string) ([]string, error) {
	url := fmt.Sprintf("%s/guilds/%s/members/%s", discordAPIRoot(), guildID, userID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := discordRoleClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, errDiscordNotInGuild
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch member failed %s %s", resp.Status, string(body))
	}

	var data struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	// Warm the shared role cache — this is the same data requireLeagueMod and
	// friends would otherwise fetch again.
	cacheDiscordRoles(guildID+":"+userID, data.Roles, true, discordRolesTTL)

	return data.Roles, nil
}

// discordAPIRoot returns the API base without a trailing slash.
func discordAPIRoot() string {
	return discordAPIURL("")
}
