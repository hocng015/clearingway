package clearingway

import (
	"fmt"
	"regexp" // Add this
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

// InitializeEmptyLeaderboards creates empty leaderboard objects for all Ultimates
// so that message IDs can be restored to them on startup
func (c *Clearingway) InitializeEmptyLeaderboards(g *Guild) {
	if !g.LeaderboardEnabled || g.LeaderboardChannelId == "" {
		return
	}

	if g.KillCountLeaderboards == nil {
		g.KillCountLeaderboards = make(map[string]*KillCountLeaderboard)
	}

	// Create empty leaderboards for all Ultimate encounters
	ultimateNames := []string{
		"The Unending Coil of Bahamut (Ultimate)",
		"The Weapon's Refrain (Ultimate)",
		"The Epic of Alexander (Ultimate)",
		"Dragonsong's Reprise (Ultimate)",
		"The Omega Protocol (Ultimate)",
		"Futures Rewritten (Ultimate)",
	}

	for _, ultimateName := range ultimateNames {
		if _, exists := g.KillCountLeaderboards[ultimateName]; !exists {
			g.KillCountLeaderboards[ultimateName] = &KillCountLeaderboard{
				Ultimate:    ultimateName,
				Entries:     []KillCountEntry{},
				LastUpdated: time.Now(),
				ChannelID:   g.LeaderboardChannelId,
				MessageID:   "", // Will be restored by RestoreLeaderboardMessages
			}
		}
	}

	fmt.Printf("Initialized %d empty leaderboards for guild %s\n", len(ultimateNames), g.Name)
}

// RestoreLeaderboardMessages scans the leaderboard channel for existing messages
// and restores both the message IDs and the actual leaderboard data for each ultimate.
func (c *Clearingway) RestoreLeaderboardMessages(s *discordgo.Session, g *Guild) error {
	if g.LeaderboardChannelId == "" {
		fmt.Printf("DEBUG: No leaderboard channel ID configured\n")
		return nil
	}

	if g.KillCountLeaderboards == nil {
		fmt.Printf("DEBUG: KillCountLeaderboards is nil\n")
		return nil
	}

	fmt.Printf("DEBUG: Found %d existing leaderboards to restore\n", len(g.KillCountLeaderboards))
	for name, lb := range g.KillCountLeaderboards {
		fmt.Printf("DEBUG: Leaderboard '%s' has MessageID '%s'\n", name, lb.MessageID)
	}

	// First, apply any manual message ID overrides from config
	if g.LeaderboardMessageOverrides != nil {
		for ultimateName, messageID := range g.LeaderboardMessageOverrides {
			if leaderboard, exists := g.KillCountLeaderboards[ultimateName]; exists {
				// Validate that the message actually exists and is accessible
				msg, err := s.ChannelMessage(g.LeaderboardChannelId, messageID)
				if err != nil {
					fmt.Printf("Warning: Manual override message ID %s for %s is invalid or inaccessible: %v\n", messageID, ultimateName, err)
					continue
				}
				leaderboard.MessageID = messageID

				// Parse existing message content to restore leaderboard data
				if len(msg.Embeds) > 0 {
					parsedEntries := c.parseLeaderboardFromEmbed(msg.Embeds[0])
					if len(parsedEntries) > 0 {
						leaderboard.Entries = parsedEntries
						leaderboard.LastUpdated = time.Now()
						fmt.Printf("Applied manual override: message ID %s for %s with %d entries\n",
							messageID, ultimateName, len(parsedEntries))
					} else {
						fmt.Printf("Applied manual override: message ID %s for %s (no entries found)\n",
							messageID, ultimateName)
					}
				}
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

					// Parse existing message content to restore leaderboard data
					parsedEntries := c.parseLeaderboardFromEmbed(embed)
					if len(parsedEntries) > 0 {
						leaderboard.Entries = parsedEntries
						leaderboard.LastUpdated = time.Now()
						fmt.Printf("Auto-detected message ID %s for %s with %d entries\n",
							msg.ID, ultimateName, len(parsedEntries))
					} else {
						fmt.Printf("Auto-detected message ID %s for %s (no entries found)\n",
							msg.ID, ultimateName)
					}

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

// parseLeaderboardFromEmbed extracts leaderboard entries from an existing Discord embed
func (c *Clearingway) parseLeaderboardFromEmbed(embed *discordgo.MessageEmbed) []KillCountEntry {
	var entries []KillCountEntry

	// Look for the "Rankings" field
	for _, field := range embed.Fields {
		if field.Name == "Rankings" {
			lines := strings.Split(field.Value, "\n")

			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				// Parse lines like: "🥇 `Dank' Tank (Gilgamesh)` - **272 kills**"
				// Use a more flexible approach - look for the pattern: `NAME (WORLD)` - **NUMBER kills**

				// Find character name and world between backticks
				start := strings.Index(line, "`")
				end := strings.LastIndex(line, "`")
				if start == -1 || end == -1 || start >= end {
					fmt.Printf("Warning: Could not find backticks in line: %s\n", line)
					continue
				}

				nameAndWorld := line[start+1 : end]

				// Split character name and world - world is in parentheses at the end
				worldStart := strings.LastIndex(nameAndWorld, "(")
				worldEnd := strings.LastIndex(nameAndWorld, ")")
				if worldStart == -1 || worldEnd == -1 || worldStart >= worldEnd {
					fmt.Printf("Warning: Could not parse character and world from: %s\n", nameAndWorld)
					continue
				}

				characterName := strings.TrimSpace(nameAndWorld[:worldStart])
				world := strings.TrimSpace(nameAndWorld[worldStart+1 : worldEnd])

				// Look for kill count using regex pattern
				// This handles both "**272 kills**" and "**272" patterns
				killCountRegex := `\*\*(\d+)(?:\s*kills)?\*\*`
				re, err := regexp.Compile(killCountRegex)
				if err != nil {
					fmt.Printf("Error compiling regex: %v\n", err)
					continue
				}

				matches := re.FindAllStringSubmatch(line, -1)
				if len(matches) == 0 {
					fmt.Printf("Warning: Could not find kill count in line: %s\n", line)
					continue
				}

				// Take the last match (in case there are multiple ** patterns)
				lastMatch := matches[len(matches)-1]
				if len(lastMatch) < 2 {
					fmt.Printf("Warning: Regex match but no capture group in: %s\n", line)
					continue
				}

				killCountStr := lastMatch[1]
				killCount := 0
				if _, err := fmt.Sscanf(killCountStr, "%d", &killCount); err != nil {
					fmt.Printf("Warning: Could not convert kill count '%s' to number: %v\n", killCountStr, err)
					continue
				}

				// Create the entry
				entry := KillCountEntry{
					CharacterName: characterName,
					World:         world,
					KillCount:     killCount,
					LastUpdate:    time.Now(),
				}

				entries = append(entries, entry)
				fmt.Printf("Parsed entry: %s (%s) - %d kills\n", characterName, world, killCount)
			}
			break // Found the Rankings field, no need to check other fields
		}
	}

	return entries
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
		return nil
	}

	// Map Ultimate names to their totem emojis
	ultimateEmojis := map[string]string{
		"The Omega Protocol (Ultimate)":           "<:toptotem:1412804998495997972>",
		"Dragonsong's Reprise (Ultimate)":         "<:dsrtotem:1412805063595921489>",
		"The Epic of Alexander (Ultimate)":        "<:teatotem:1412805183909527552>",
		"Futures Rewritten (Ultimate)":            "<:frutotem:1412805130255863818>",
		"The Weapon's Refrain (Ultimate)":         "<:uwutotem:1412805291568791752>",
		"The Unending Coil of Bahamut (Ultimate)": "<:ucobtotem:1412805358518407179>",
	}

	// Get the emoji for this ultimate, default to empty string if not found
	emoji := ultimateEmojis[leaderboard.Ultimate]

	// Build the embed with emoji at beginning and end of title
	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s 🏆 %s Kill Count Leaderboard %s", emoji, leaderboard.Ultimate, emoji),
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

	// Initialize LeaderboardMessageIds if it doesn't exist
	if g.LeaderboardMessageIds == nil {
		g.LeaderboardMessageIds = make(map[string]string)
	}

	messageId := ""

	// Try the new MessageID field first
	if leaderboard.MessageID != "" {
		messageId = leaderboard.MessageID
	} else if g.LeaderboardMessageIds[leaderboard.Ultimate] != "" {
		// Fall back to old system
		messageId = g.LeaderboardMessageIds[leaderboard.Ultimate]
		leaderboard.MessageID = messageId // Migrate to new system
	}

	if messageId != "" {
		// Try to edit existing message
		_, err := s.ChannelMessageEditEmbed(leaderboard.ChannelID, messageId, embed)
		if err != nil {
			fmt.Printf("Failed to edit message ID %s for %s: %v\n", messageId, leaderboard.Ultimate, err)
			// Create new message if edit fails
			msg, err := s.ChannelMessageSendEmbed(leaderboard.ChannelID, embed)
			if err != nil {
				return fmt.Errorf("Could not post leaderboard: %w", err)
			}
			leaderboard.MessageID = msg.ID
			g.LeaderboardMessageIds[leaderboard.Ultimate] = msg.ID
			fmt.Printf("Created new message ID %s for %s\n", msg.ID, leaderboard.Ultimate)
		} else {
			fmt.Printf("Successfully updated existing message ID %s for %s\n", messageId, leaderboard.Ultimate)
		}
	} else {
		// Create new message
		msg, err := s.ChannelMessageSendEmbed(leaderboard.ChannelID, embed)
		if err != nil {
			return fmt.Errorf("Could not post leaderboard: %w", err)
		}
		leaderboard.MessageID = msg.ID
		g.LeaderboardMessageIds[leaderboard.Ultimate] = msg.ID
		fmt.Printf("Created first message ID %s for %s\n", msg.ID, leaderboard.Ultimate)
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
