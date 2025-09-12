package clearingway

import (
	"fmt"
	"strings"

	"github.com/Veraticus/clearingway/internal/discord"
	"github.com/bwmarrin/discordgo"
)

// Command to manually refresh all leaderboards
var LeaderboardCommand = &discordgo.ApplicationCommand{
	Name:                     "leaderboard",
	Description:              "Manage kill count leaderboards",
	DefaultMemberPermissions: &adminPermission,
	Options: []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "show",
			Description: "Show the current leaderboard for an ultimate",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "ultimate",
					Description: "The ultimate to show",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{
							Name:  "UCoB",
							Value: "The Unending Coil of Bahamut (Ultimate)",
						},
						{
							Name:  "UWU",
							Value: "The Weapon's Refrain (Ultimate)",
						},
						{
							Name:  "TEA",
							Value: "The Epic of Alexander (Ultimate)",
						},
						{
							Name:  "DSR",
							Value: "Dragonsong's Reprise (Ultimate)",
						},
						{
							Name:  "TOP",
							Value: "The Omega Protocol (Ultimate)",
						},
						{
							Name:  "FRU",
							Value: "Futures Rewritten (Ultimate)",
						},
					},
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "refresh",
			Description: "Refresh all leaderboards",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "clear",
			Description: "Clear a leaderboard",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "ultimate",
					Description: "The ultimate to clear",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{
							Name:  "UCoB",
							Value: "The Unending Coil of Bahamut (Ultimate)",
						},
						{
							Name:  "UWU",
							Value: "The Weapon's Refrain (Ultimate)",
						},
						{
							Name:  "TEA",
							Value: "The Epic of Alexander (Ultimate)",
						},
						{
							Name:  "DSR",
							Value: "Dragonsong's Reprise (Ultimate)",
						},
						{
							Name:  "TOP",
							Value: "The Omega Protocol (Ultimate)",
						},
						{
							Name:  "FRU",
							Value: "Futures Rewritten (Ultimate)",
						},
					},
				},
			},
		},
	},
}

func (c *Clearingway) LeaderboardManagement(s *discordgo.Session, i *discordgo.InteractionCreate) {
	g, ok := c.Guilds.Guilds[i.GuildID]
	if !ok {
		fmt.Printf("Interaction received from guild %s with no configuration!\n", i.GuildID)
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		discord.StartInteraction(s, i.Interaction, "Please specify a subcommand.")
		return
	}

	subcommand := options[0]

	switch subcommand.Name {
	case "show":
		c.ShowLeaderboard(s, i, g, subcommand.Options)
	case "refresh":
		c.RefreshAllLeaderboards(s, i, g)
	case "clear":
		c.ClearLeaderboard(s, i, g, subcommand.Options)
	}
}

func (c *Clearingway) ShowLeaderboard(s *discordgo.Session, i *discordgo.InteractionCreate, g *Guild, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		discord.StartInteraction(s, i.Interaction, "Please specify an ultimate.")
		return
	}

	ultimate := options[0].StringValue()

	if g.KillCountLeaderboards == nil || g.KillCountLeaderboards[ultimate] == nil {
		discord.StartInteraction(s, i.Interaction, fmt.Sprintf("No leaderboard exists for %s yet.", ultimate))
		return
	}

	leaderboard := g.KillCountLeaderboards[ultimate]

	// Build the response
	response := strings.Builder{}
	response.WriteString(fmt.Sprintf("**%s Kill Count Leaderboard**\n\n", ultimate))

	if len(leaderboard.Entries) == 0 {
		response.WriteString("No entries yet!")
	} else {
		for i, entry := range leaderboard.Entries {
			medal := ""
			switch i {
			case 0:
				medal = "🥇"
			case 1:
				medal = "🥈"
			case 2:
				medal = "🥉"
			default:
				medal = fmt.Sprintf("#%d", i+1)
			}

			response.WriteString(fmt.Sprintf("%s `%s (%s)` - **%d kills**\n",
				medal, entry.CharacterName, entry.World, entry.KillCount))

			if i >= 9 { // Show top 10
				break
			}
		}
	}

	discord.StartInteraction(s, i.Interaction, response.String())
}

func (c *Clearingway) RefreshAllLeaderboards(s *discordgo.Session, i *discordgo.InteractionCreate, g *Guild) {
	err := discord.StartInteraction(s, i.Interaction, "Refreshing all leaderboards...")
	if err != nil {
		fmt.Printf("Error sending Discord message: %v\n", err)
		return
	}

	if g.KillCountLeaderboards == nil || len(g.KillCountLeaderboards) == 0 {
		discord.ContinueInteraction(s, i.Interaction, "No leaderboards to refresh.")
		return
	}

	for _, leaderboard := range g.KillCountLeaderboards {
		err := c.PostLeaderboard(s, g, leaderboard)
		if err != nil {
			fmt.Printf("Error refreshing leaderboard for %s: %v\n", leaderboard.Ultimate, err)
		}
	}

	discord.ContinueInteraction(s, i.Interaction, "✅ All leaderboards have been refreshed!")
}

func (c *Clearingway) ClearLeaderboard(s *discordgo.Session, i *discordgo.InteractionCreate, g *Guild, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		discord.StartInteraction(s, i.Interaction, "Please specify an ultimate.")
		return
	}

	ultimate := options[0].StringValue()

	if g.KillCountLeaderboards == nil {
		g.KillCountLeaderboards = make(map[string]*KillCountLeaderboard)
	}

	delete(g.KillCountLeaderboards, ultimate)

	if g.LeaderboardMessageIds != nil {
		delete(g.LeaderboardMessageIds, ultimate)
	}

	discord.StartInteraction(s, i.Interaction, fmt.Sprintf("✅ Cleared the leaderboard for %s.", ultimate))
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
