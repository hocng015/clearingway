package clearingway

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Veraticus/clearingway/internal/discord"
	"github.com/Veraticus/clearingway/internal/fflogs"
	"github.com/Veraticus/clearingway/internal/ffxiv"
	"github.com/Veraticus/clearingway/internal/lodestone"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// KillCountEntry represents a player's kill count for ranking
type KillCountEntry struct {
	CharacterName string
	World         string
	KillCount     int
	LastUpdate    time.Time
}

// KillCountLeaderboard manages the leaderboard data
type KillCountLeaderboard struct {
	Ultimate    string
	Entries     []KillCountEntry
	LastUpdated time.Time
	ChannelID   string // Channel to post the leaderboard
	MessageID   string // Store the message ID directly in the leaderboard
}

// RestoreLeaderboardMessages scans the leaderboard channel for existing messages
// and restores the message IDs for each ultimate. Call this on bot startup.
// It first tries to use manual config overrides, then falls back to automatic detection.
func (c *Clearingway) RestoreLeaderboardMessages(s *discordgo.Session, g *Guild) error {
	if g.LeaderboardChannelId == "" {
		return nil
	}

	if g.KillCountLeaderboards == nil {
		return nil
	}

	// First, apply any manual message ID overrides from config
	if g.LeaderboardMessageOverrides != nil {
		for ultimateName, messageID := range g.LeaderboardMessageOverrides {
			if leaderboard, exists := g.KillCountLeaderboards[ultimateName]; exists {
				// Validate that the message actually exists and is accessible
				_, err := s.ChannelMessage(g.LeaderboardChannelId, messageID)
				if err != nil {
					fmt.Printf("Warning: Manual override message ID %s for %s is invalid or inaccessible: %v\n", messageID, ultimateName, err)
					continue
				}
				leaderboard.MessageID = messageID
				fmt.Printf("Applied manual override: message ID %s for ultimate %s\n", messageID, ultimateName)
			}
		}
	}

	// Then, for any leaderboards without message IDs, try automatic detection
	leaderboardsNeedingIDs := make(map[string]*KillCountLeaderboard)
	for ultimateName, leaderboard := range g.KillCountLeaderboards {
		if leaderboard.MessageID == "" {
			leaderboardsNeedingIDs[ultimateName] = leaderboard
		}
	}

	if len(leaderboardsNeedingIDs) == 0 {
		fmt.Printf("All leaderboards have message IDs (manual or previously detected)\n")
		return nil
	}

	fmt.Printf("Attempting automatic detection for %d leaderboards...\n", len(leaderboardsNeedingIDs))

	// Get recent messages from the leaderboard channel for automatic detection
	messages, err := s.ChannelMessages(g.LeaderboardChannelId, 100, "", "", "")
	if err != nil {
		return fmt.Errorf("failed to get channel messages for automatic detection: %w", err)
	}

	// Look for messages that match leaderboard format
	for _, msg := range messages {
		if len(msg.Embeds) > 0 && msg.Author.ID == s.State.User.ID {
			embed := msg.Embeds[0]
			// Check each ultimate that still needs a message ID
			for ultimateName, leaderboard := range leaderboardsNeedingIDs {
				if strings.Contains(embed.Title, ultimateName) || strings.Contains(embed.Title, leaderboard.Ultimate) {
					leaderboard.MessageID = msg.ID
					fmt.Printf("Auto-detected message ID %s for ultimate %s\n", msg.ID, ultimateName)
					delete(leaderboardsNeedingIDs, ultimateName) // Remove from list
					break
				}
			}
		}
	}

	// Report any leaderboards that still don't have message IDs
	if len(leaderboardsNeedingIDs) > 0 {
		fmt.Printf("Warning: Could not find existing messages for %d leaderboards:\n", len(leaderboardsNeedingIDs))
		for ultimateName := range leaderboardsNeedingIDs {
			fmt.Printf("  - %s (new messages will be created)\n", ultimateName)
		}
	}

	return nil
}

func (c *Clearingway) Count(s *discordgo.Session, i *discordgo.InteractionCreate) {
	g, ok := c.Guilds.Guilds[i.GuildID]
	if !ok {
		fmt.Printf("Interaction received from guild %s with no configuration!\n", i.GuildID)
		return
	}

	// Defer the interaction immediately to avoid timeout
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		fmt.Printf("Error deferring interaction: %v\n", err)
		return
	}

	// Retrieve all the options sent to the command
	options := i.ApplicationCommandData().Options
	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
	for _, opt := range options {
		optionMap[opt.Name] = opt
	}

	var world string
	var firstName string
	var lastName string
	var ultimate string

	if option, ok := optionMap["world"]; ok {
		world = option.StringValue()
	}
	if option, ok := optionMap["first-name"]; ok {
		firstName = option.StringValue()
	}
	if option, ok := optionMap["last-name"]; ok {
		lastName = option.StringValue()
	}
	if option, ok := optionMap["ultimate"]; ok {
		ultimate = option.StringValue()
	}

	c.CountHelper(s, i, g, world, firstName, lastName, ultimate)
}

func (c *Clearingway) CountHelper(s *discordgo.Session, i *discordgo.InteractionCreate, g *Guild, world string, firstName string, lastName string, ultimate string) {
	// Use FollowupMessageCreate for the first message since we deferred
	if len(world) == 0 || len(firstName) == 0 || len(lastName) == 0 || len(ultimate) == 0 {
		_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "`/count` command failed! Please input your world, first name, last name, and ultimate.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		if err != nil {
			fmt.Printf("Error sending Discord message: %v\n", err)
		}
		return
	}

	title := cases.Title(language.AmericanEnglish)
	world = title.String(cleanWorld(world))
	firstName = title.String(firstName)
	lastName = title.String(lastName)

	if !ffxiv.IsWorld(world) {
		discord.ContinueInteraction(s, i.Interaction,
			fmt.Sprintf("`%s` is not a valid world! Make sure you spelled your world name properly.", world),
		)
		return
	}

	// Find the ultimate encounter
	var targetEncounter *Encounter
	for _, e := range UltimateEncounters.Encounters {
		if e.Name == ultimate {
			targetEncounter = g.Encounters.ForName(e.Name)
			break
		}
	}

	if targetEncounter == nil {
		discord.ContinueInteraction(s, i.Interaction,
			fmt.Sprintf("`%s` is not a valid ultimate!", ultimate),
		)
		return
	}

	discord.ContinueInteraction(s, i.Interaction,
		fmt.Sprintf("Finding `%s %s (%s)` in the Lodestone...", firstName, lastName, world),
	)

	char, err := g.Characters.Init(world, firstName, lastName)
	if err != nil {
		discord.ContinueInteraction(s, i.Interaction, err.Error())
		return
	}

	err = c.Fflogs.SetCharacterLodestoneID(char)
	if err != nil {
		discord.ContinueInteraction(s, i.Interaction,
			fmt.Sprintf(
				"Error finding this character's Lodestone ID from FF Logs: %v\nTo make lookups faster in the future, please link your character in FF Logs to the Lodestone here: https://www.fflogs.com/lodestone/import",
				err,
			))

		err = lodestone.SetCharacterLodestoneID(char)
		if err != nil {
			discord.ContinueInteraction(s, i.Interaction,
				fmt.Sprintf(
					"Error finding this character's Lodestone ID in the Lodestone: %v\nIf your character name is short or very common this can frequently fail. Please link your character in FF Logs to the Lodestone here: https://www.fflogs.com/lodestone/import",
					err,
				))
			return
		}
	}

	discord.ContinueInteraction(s, i.Interaction,
		fmt.Sprintf("Verifying ownership of `%s (%s)`...", char.Name(), char.World),
	)

	discordId := i.Member.User.ID
	isOwner, err := lodestone.CharacterIsOwnedByDiscordUser(char, discordId)
	if err != nil {
		discord.ContinueInteraction(s, i.Interaction, err.Error())
		return
	}
	if !isOwner {
		discord.ContinueInteraction(s, i.Interaction,
			fmt.Sprintf(
				"I could not verify your ownership of `%s (%s)`!\nIf this is your character, add the following code to your Lodestone profile and try again:\n\n**%s**\n\nYou can edit your Lodestone profile at https://na.finalfantasyxiv.com/lodestone/my/setting/profile/",
				char.Name(),
				char.World,
				char.LodestoneSlug(discordId),
			),
		)
		return
	}

	discord.ContinueInteraction(s, i.Interaction,
		fmt.Sprintf("Retrieving kill count for `%s (%s)` in %s...", char.Name(), char.World, ultimate),
	)

	// Get the kill count for this specific ultimate
	killCount, err := c.GetKillCountForUltimate(char, targetEncounter)
	if err != nil {
		discord.ContinueInteraction(s, i.Interaction,
			fmt.Sprintf("Could not retrieve kill count: %s", err),
		)
		return
	}

	// Update the leaderboard
	err = c.UpdateKillCountLeaderboard(s, g, char, ultimate, killCount)
	if err != nil {
		fmt.Printf("Error updating leaderboard: %v\n", err)
	}

	discord.ContinueInteraction(s, i.Interaction,
		fmt.Sprintf("✅ **Kill count recorded!**\n\n`%s (%s)` has **%d kills** in %s.\n\nThe leaderboard has been updated!",
			char.Name(), char.World, killCount, ultimate),
	)
}

func (c *Clearingway) GetKillCountForUltimate(char *ffxiv.Character, encounter *Encounter) (int, error) {
	rankingsToGet := []*fflogs.RankingToGet{
		{IDs: encounter.Ids, Difficulty: encounter.DifficultyInt()},
	}

	rankings, err := c.Fflogs.GetRankingsForCharacter(rankingsToGet, char)
	if err != nil {
		return 0, fmt.Errorf("Error retrieving encounter rankings: %w", err)
	}

	totalKills := 0
	for _, encounterId := range encounter.Ids {
		ranking, ok := rankings.Rankings[encounterId]
		if !ok {
			continue
		}
		if !ranking.Cleared() {
			continue
		}
		totalKills += ranking.TotalKills
	}

	return totalKills, nil
}

func (c *Clearingway) UpdateKillCountLeaderboard(s *discordgo.Session, g *Guild, char *ffxiv.Character, ultimate string, killCount int) error {
	// Initialize leaderboard if it doesn't exist
	if g.KillCountLeaderboards == nil {
		g.KillCountLeaderboards = make(map[string]*KillCountLeaderboard)
	}

	leaderboard, exists := g.KillCountLeaderboards[ultimate]
	if !exists {
		leaderboard = &KillCountLeaderboard{
			Ultimate:    ultimate,
			Entries:     []KillCountEntry{},
			LastUpdated: time.Now(),
			ChannelID:   g.LeaderboardChannelId,
			MessageID:   "", // Will be set when message is created
		}
		g.KillCountLeaderboards[ultimate] = leaderboard
	}

	// Update or add the entry
	found := false
	for i, entry := range leaderboard.Entries {
		if entry.CharacterName == char.Name() && entry.World == char.World {
			leaderboard.Entries[i].KillCount = killCount
			leaderboard.Entries[i].LastUpdate = time.Now()
			found = true
			break
		}
	}

	if !found {
		leaderboard.Entries = append(leaderboard.Entries, KillCountEntry{
			CharacterName: char.Name(),
			World:         char.World,
			KillCount:     killCount,
			LastUpdate:    time.Now(),
		})
	}

	// Sort entries by kill count (descending)
	sort.Slice(leaderboard.Entries, func(i, j int) bool {
		return leaderboard.Entries[i].KillCount > leaderboard.Entries[j].KillCount
	})

	leaderboard.LastUpdated = time.Now()

	// Post the updated leaderboard
	return c.PostLeaderboard(s, g, leaderboard)
}

func (c *Clearingway) PostLeaderboard(s *discordgo.Session, g *Guild, leaderboard *KillCountLeaderboard) error {
	if leaderboard.ChannelID == "" {
		// If no leaderboard channel is configured, skip posting
		return nil
	}

	// Build the leaderboard message
	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🏆 %s Kill Count Leaderboard", leaderboard.Ultimate),
		Description: "Top raiders by total kills",
		Color:       0x00ff00,
		Timestamp:   leaderboard.LastUpdated.Format(time.RFC3339),
		Fields:      []*discordgo.MessageEmbedField{},
	}

	// Add top 10 entries
	maxEntries := 10
	if len(leaderboard.Entries) < maxEntries {
		maxEntries = len(leaderboard.Entries)
	}

	leaderboardText := strings.Builder{}
	for i := 0; i < maxEntries; i++ {
		entry := leaderboard.Entries[i]
		medal := ""
		switch i {
		case 0:
			medal = "🥇"
		case 1:
			medal = "🥈"
		case 2:
			medal = "🥉"
		default:
			medal = fmt.Sprintf("**#%d**", i+1)
		}

		leaderboardText.WriteString(fmt.Sprintf("%s `%s (%s)` - **%d kills**\n",
			medal, entry.CharacterName, entry.World, entry.KillCount))
	}

	if leaderboardText.Len() > 0 {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:  "Rankings",
			Value: leaderboardText.String(),
		})
	}

	// Check if we should update an existing message or create a new one
	if leaderboard.MessageID != "" {
		// Try to edit the existing message
		_, err := s.ChannelMessageEditEmbed(leaderboard.ChannelID, leaderboard.MessageID, embed)
		if err != nil {
			fmt.Printf("Failed to edit existing message (ID: %s), creating new one: %v\n", leaderboard.MessageID, err)
			// If editing fails, create a new message
			msg, err := s.ChannelMessageSendEmbed(leaderboard.ChannelID, embed)
			if err != nil {
				return fmt.Errorf("Could not post leaderboard: %w", err)
			}
			leaderboard.MessageID = msg.ID
		}
	} else {
		// Create a new message
		msg, err := s.ChannelMessageSendEmbed(leaderboard.ChannelID, embed)
		if err != nil {
			return fmt.Errorf("Could not post leaderboard: %w", err)
		}
		leaderboard.MessageID = msg.ID
	}

	// Remove the old LeaderboardMessageIds if it exists (for migration purposes)
	if g.LeaderboardMessageIds != nil {
		delete(g.LeaderboardMessageIds, leaderboard.Ultimate)
		if len(g.LeaderboardMessageIds) == 0 {
			g.LeaderboardMessageIds = nil
		}
	}

	return nil
}

// Optional: Add a batch update command for updating multiple characters at once
func (c *Clearingway) BatchUpdateKillCounts(s *discordgo.Session, g *Guild, ultimate string) error {
	// This could be used to update all registered characters' kill counts
	// periodically or on demand

	for characterKey, char := range g.Characters.Characters {
		_ = characterKey // Use this if needed

		// Find the ultimate encounter
		var targetEncounter *Encounter
		for _, e := range UltimateEncounters.Encounters {
			if e.Name == ultimate {
				targetEncounter = g.Encounters.ForName(e.Name)
				break
			}
		}

		if targetEncounter == nil {
			continue
		}

		killCount, err := c.GetKillCountForUltimate(char, targetEncounter)
		if err != nil {
			fmt.Printf("Error getting kill count for %s: %v\n", char.Name(), err)
			continue
		}

		// Update the leaderboard entry
		if g.KillCountLeaderboards == nil {
			g.KillCountLeaderboards = make(map[string]*KillCountLeaderboard)
		}

		leaderboard, exists := g.KillCountLeaderboards[ultimate]
		if !exists {
			leaderboard = &KillCountLeaderboard{
				Ultimate:  ultimate,
				Entries:   []KillCountEntry{},
				ChannelID: g.LeaderboardChannelId,
			}
			g.KillCountLeaderboards[ultimate] = leaderboard
		}

		// Update or add the entry
		found := false
		for i, entry := range leaderboard.Entries {
			if entry.CharacterName == char.Name() && entry.World == char.World {
				leaderboard.Entries[i].KillCount = killCount
				found = true
				break
			}
		}

		if !found && killCount > 0 {
			leaderboard.Entries = append(leaderboard.Entries, KillCountEntry{
				CharacterName: char.Name(),
				World:         char.World,
				KillCount:     killCount,
			})
		}
	}

	// Post the updated leaderboard
	if leaderboard, exists := g.KillCountLeaderboards[ultimate]; exists {
		return c.PostLeaderboard(s, g, leaderboard)
	}

	return nil
}
