package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Veraticus/clearingway/internal/clearingway"
	"github.com/Veraticus/clearingway/internal/discord"
	"github.com/Veraticus/clearingway/internal/fflogs"
	"github.com/Veraticus/clearingway/internal/lodestone"

	"gopkg.in/yaml.v3"
)

func main() {
	discordToken, ok := os.LookupEnv("DISCORD_TOKEN")
	if !ok {
		panic("You must supply a DISCORD_TOKEN to start!")
	}
	discordToken = strings.TrimSpace(discordToken)

	fflogsClientId, ok := os.LookupEnv("FFLOGS_CLIENT_ID")
	if !ok {
		panic("You must supply a FFLOGS_CLIENT_ID to start!")
	}
	fflogsClientId = strings.TrimSpace(fflogsClientId)

	fflogsClientSecret, ok := os.LookupEnv("FFLOGS_CLIENT_SECRET")
	if !ok {
		panic("You must supply a FFLOGS_CLIENT_SECRET to start!")
	}
	fflogsClientSecret = strings.TrimSpace(fflogsClientSecret)

	c := &clearingway.Clearingway{
		Config: &clearingway.Config{},
		Fflogs: fflogs.Init(fflogsClientId, fflogsClientSecret),
		Discord: &discord.Discord{
			Token: discordToken,
		},
	}

	config, err := os.ReadFile("./config.yaml")
	if err != nil {
		panic(fmt.Errorf("Could not read config.yaml: %w", err))
	}
	err = yaml.Unmarshal(config, &c.Config)
	if err != nil {
		panic(fmt.Errorf("Could not unmarshal config.yaml: %w", err))
	}

	c.Init()

	fmt.Printf("Clearingway is: %+v\n", c)
	for _, guild := range c.Guilds.Guilds {
		fmt.Printf("Guild added: %+v\n", guild)

		if guild.EncounterRoles != nil {
			fmt.Printf("Encounter roles: %+v\n", guild.EncounterRoles.Roles)
		}

		if guild.AchievementRoles != nil {
			fmt.Printf("Achievement roles: %+v\n", guild.AchievementRoles.Roles)
		}

		if guild.RelevantParsingRoles != nil {
			fmt.Printf("Relevant parsing roles: %+v\n", guild.RelevantParsingRoles.Roles)
		}

		if guild.RelevantFlexingRoles != nil {
			fmt.Printf("Relevant flexing roles: %+v\n", guild.RelevantFlexingRoles.Roles)
		}

		if guild.LegendRoles != nil {
			fmt.Printf("Legend roles: %+v\n", guild.LegendRoles.Roles)
		}

		if guild.UltimateFlexingRoles != nil {
			fmt.Printf("Ultimate flexing roles: %+v\n", guild.UltimateFlexingRoles.Roles)
		}

		if guild.DatacenterRoles != nil {
			fmt.Printf("Datacenter roles: %+v\n", guild.DatacenterRoles.Roles)
		}
	}

	fmt.Printf("Starting Discord...\n")
	err = c.Discord.Start()
	if err != nil {
		panic(fmt.Errorf("Could not instantiate Discord: %w", err))
	}
	defer c.Discord.Session.Close()

	var arg string
	args := os.Args[1:]
	if len(args) == 0 {
		arg = ""
	} else {
		arg = args[0]
	}
	switch arg {
	case "clears":
		clears(c)
	case "prog":
		prog(c)
	default:
		start(c)
	}
}

func start(c *clearingway.Clearingway) {
	c.Discord.Session.AddHandler(c.DiscordReady)
	err := c.Discord.Session.Open()
	if err != nil {
		panic(fmt.Errorf("Could not open Discord session: %f", err))
	}
	for !c.Ready {
		fmt.Printf("Waiting for Clearingway to be ready...\n")
		time.Sleep(2 * time.Second)
	}

	// NEW: Restore leaderboard message IDs after bot is ready
	fmt.Printf("Restoring leaderboard message IDs...\n")
	for guildId, guild := range c.Guilds.Guilds {
		if guild.LeaderboardEnabled && guild.LeaderboardChannelId != "" {
			err := c.RestoreLeaderboardMessages(c.Discord.Session, guild)
			if err != nil {
				fmt.Printf("Error restoring leaderboard messages for guild %s (%s): %v\n",
					guild.Name, guildId, err)
			} else {
				fmt.Printf("Successfully processed leaderboard message restoration for guild %s\n",
					guild.Name)
			}
		}
	}
	fmt.Printf("Leaderboard message restoration complete.\n")

	c.Discord.Session.AddHandler(c.InteractionCreate)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func clears(c *clearingway.Clearingway) {
	if len(os.Args) != 7 {
		panic("Provide a world, firstName, lastName, guildId, and discordId!")
	}
	world := os.Args[2]
	firstName := os.Args[3]
	lastName := os.Args[4]
	guildId := os.Args[5]
	discordId := os.Args[6]

	guild, ok := c.Guilds.Guilds[guildId]
	if !ok {
		panic(fmt.Sprintf("Guild %s not setup in config.yaml but you tried to run me in it!", guildId))
	}

	c.Discord.Session.AddHandler(c.DiscordReady)
	err := c.Discord.Session.Open()
	if err != nil {
		panic(fmt.Errorf("Could not open Discord session: %f", err))
	}

	for !c.Ready {
		fmt.Printf("Waiting for Clearingway to be ready...\n")
		time.Sleep(2 * time.Second)
	}

	char, err := guild.Characters.Init(world, firstName, lastName)
	if err != nil {
		panic(err)
	}

	err = c.Fflogs.SetCharacterLodestoneID(char)
	if err != nil {
		fmt.Printf("Could not find character in FF Logs: %+v\n", err)
		err = lodestone.SetCharacterLodestoneID(char)
		if err != nil {
			panic(fmt.Errorf("Could not find character in the Lodestone: %+v", err))
		}
	}

	isOwner, err := lodestone.CharacterIsOwnedByDiscordUser(char, discordId)
	if err != nil {
		panic(err)
	}
	if !isOwner {
		panic("That character is not owned by that Discord ID!")
	}

	roleTexts, err := c.UpdateClearsForCharacterInGuild(char, discordId, guild)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Character %s (%s) clears updated in guild %s.\n", char.Name(), char.World, guild.Name)

	for _, roleText := range roleTexts {
		fmt.Printf(roleText + "\n")
	}
}

func prog(c *clearingway.Clearingway) {
	if len(os.Args) != 8 {
		panic("Provide a world, firstName, lastName, guildId, discordId, and a report ID or url!")
	}
	world := os.Args[2]
	firstName := os.Args[3]
	lastName := os.Args[4]
	guildId := os.Args[5]
	discordId := os.Args[6]
	reportId := os.Args[7]

	reportId = clearingway.CleanReportId(reportId)

	guild, ok := c.Guilds.Guilds[guildId]
	if !ok {
		panic(fmt.Sprintf("Guild %s not setup in config.yaml but you tried to run me in it!", guildId))
	}

	c.Discord.Session.AddHandler(c.DiscordReady)
	err := c.Discord.Session.Open()
	if err != nil {
		panic(fmt.Errorf("Could not open Discord session: %f", err))
	}

	for !c.Ready {
		fmt.Printf("Waiting for Clearingway to be ready...\n")
		time.Sleep(2 * time.Second)
	}

	char, err := guild.Characters.Init(world, firstName, lastName)
	if err != nil {
		panic(err)
	}

	err = c.Fflogs.SetCharacterLodestoneID(char)
	if err != nil {
		fmt.Printf("Could not find character in FF Logs: %+v\n", err)
		err = lodestone.SetCharacterLodestoneID(char)
		if err != nil {
			panic(fmt.Errorf("Could not find character in the Lodestone: %+v", err))
		}
	}

	isOwner, err := lodestone.CharacterIsOwnedByDiscordUser(char, discordId)
	if err != nil {
		panic(err)
	}
	if !isOwner {
		panic("That character is not owned by that Discord ID!")
	}

	progTexts, err := c.UpdateProgForCharacterInGuild(reportId, char, discordId, guild)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Character %s (%s) prog updated in guild %s.\n", char.Name(), char.World, guild.Name)

	for _, progText := range progTexts {
		fmt.Printf(progText + "\n")
	}
}
