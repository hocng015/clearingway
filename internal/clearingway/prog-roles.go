package clearingway

import (
	"fmt"
	"strings"
)

func ProgRoles(rs []*ConfigRole, e *Encounter) *Roles {
	roles := &Roles{Roles: []*Role{}}
	for i, r := range rs {
		phase := i
		role := &Role{
			Name: r.Name, Color: r.Color, Type: ProgRole,
			Hoist: r.Hoist, Mention: r.Mention,
			Description: fmt.Sprintf("Reached phase %d (%s) in prog.", phase+1, r.Name),
		}
		roles.Roles = append(roles.Roles, role)
	}
	roles.ShouldApply = func(opts *ShouldApplyOpts) (bool, string, []*Role, []*Role) {
		fmt.Printf("ProgRoles ShouldApply called with %d existing roles\n", len(opts.ExistingRoles.Roles))
		fmt.Printf("Available prog roles: %d\n", len(roles.Roles))
		
		if opts.Fights == nil || len(opts.Fights.Fights) == 0 {
			fmt.Printf("No fights provided to ShouldApply\n")
			return false, "No valid fights found in provided report!", nil, nil
		}
		
		// Find existing PROG roles only (ignore cleared roles for prog logic)
		var existingProgRole *Role
		var existingProgRoleIndex int = -1
		for _, existingRole := range opts.ExistingRoles.Roles {
			// Only check for actual prog roles, not cleared roles
			if existingRole.Type == ProgRole {
				ok, i := roles.IndexOfRole(existingRole)
				if ok {
					existingProgRole = existingRole
					existingProgRoleIndex = i
					break // Take the highest prog role found
				}
			}
		}
		
		// Find the furthest prog provided in the fights from the report
		furthestFight := opts.Fights.FurthestFight()
		
		// Debug logging
		fmt.Printf("Furthest fight: ID=%d, Kill=%t, LastPhaseIndex=%d\n", furthestFight.ID, furthestFight.Kill, furthestFight.LastPhaseIndex)
		
		var furthestProgRole *Role
		var furthestProgRoleIndex int
		
		// Create return message.
		messageString := strings.Builder{}
		messageString.WriteString(fmt.Sprintf("⮕ Fight %d\n", furthestFight.ID))
		
		// For prog roles, we only care about the highest phase reached, not kills
		// Even if there's a kill, we want to assign the prog role for the phase reached
		if furthestFight.Kill {
			// If there's a kill, assign prog role based on the phase where the kill happened
			furthestProgRoleIndex = furthestFight.LastPhaseIndex
			if furthestProgRoleIndex >= len(roles.Roles) {
				// If the phase index exceeds available prog roles, assign the highest prog role
				furthestProgRoleIndex = len(roles.Roles) - 1
			}
			furthestProgRole = roles.Roles[furthestProgRoleIndex]
			fmt.Printf("Report contains a KILL at phase %d - should assign prog role: %s\n", furthestFight.LastPhaseIndex, furthestProgRole.Name)
		} else {
			furthestProgRoleIndex = furthestFight.LastPhaseIndex
			if furthestProgRoleIndex >= len(roles.Roles) {
				furthestProgRoleIndex = len(roles.Roles) - 1
			}
			furthestProgRole = roles.Roles[furthestProgRoleIndex]
			fmt.Printf("Report shows prog to phase %d - should assign prog role: %s\n", furthestProgRoleIndex, furthestProgRole.Name)
		}
		
		// More debug logging
		if existingProgRole != nil {
			fmt.Printf("User has existing prog role: %s (index %d)\n", existingProgRole.Name, existingProgRoleIndex)
			fmt.Printf("Report shows furthest: %s (index %d)\n", furthestProgRole.Name, furthestProgRoleIndex)
		} else {
			fmt.Printf("User has NO existing prog role\n")
		}
		
		// Bail out if the furthest prog point in the fight is less than or equal to one
		// the user already possesses
		if existingProgRole != nil && furthestProgRoleIndex <= existingProgRoleIndex {
			if furthestProgRoleIndex < existingProgRoleIndex {
				messageString.WriteString(fmt.Sprintf(
					"You already have a prog role further than the furthest prog in this report! Your existing prog point is `%s` (phase %d), and the furthest prog point seen by you in this report is `%s` (phase %d).",
					existingProgRole.Name,
					existingProgRoleIndex+1,
					furthestProgRole.Name,
					furthestProgRoleIndex+1,
				))
			} else {
				messageString.WriteString(fmt.Sprintf(
					"Your furthest prog point, `%s` (phase %d), is the same as the furthest prog point in this report.",
					existingProgRole.Name,
					existingProgRoleIndex+1,
				))
			}
			fmt.Printf("BAILING: User has same or higher prog than report shows\n")
			return false, messageString.String(), nil, nil
		}
		
		// Looks like we have some real prog to give!
		// Remove all lower prog roles
		var lowerRoles []*Role
		if existingProgRoleIndex >= 0 {
			// Remove the existing prog role and all lower ones
			lowerRoles = roles.Roles[0:existingProgRoleIndex+1]
		}
		
		messageString.WriteString(fmt.Sprintf(
			"Your furthest prog point is now `%s` (phase %d).\n",
			furthestProgRole.Name,
			furthestProgRoleIndex+1,
		))
		
		fmt.Printf("SUCCESS: Should apply role %s\n", furthestProgRole.Name)
		return true, messageString.String(), []*Role{furthestProgRole}, lowerRoles
	}
	return roles
}
