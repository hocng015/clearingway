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
   	
   	// Determine if any existing prog role exists
   	var existingProgRole *Role
   	var existingProgRoleIndex int
   	for _, existingRole := range opts.ExistingRoles.Roles {
   		if existingRole.Type == ClearedRole {
   			existingProgRoleIndex = len(roles.Roles) + 1
   			existingProgRole = e.Roles[ClearedRole]
   		}
   		ok, i := roles.IndexOfRole(existingRole)
   		if ok {
   			existingProgRole = existingRole
   			existingProgRoleIndex = i
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
   	
   	// Does this report contain a kill?
   	if furthestFight.Kill {
   		furthestProgRoleIndex = len(roles.Roles) + 1
   		furthestProgRole = e.Roles[ClearedRole]
   		fmt.Printf("Report contains a KILL - should assign cleared role\n")
   	} else {
   		furthestProgRoleIndex = furthestFight.LastPhaseIndex
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
   	
   	// Bail out if the furthest prog point in the fight is less than one
   	// the user already possesses
   	if existingProgRole != nil && furthestProgRoleIndex < existingProgRoleIndex {
   		messageString.WriteString(fmt.Sprintf(
   			"You already have a prog role further than the furthest prog in this report! Your existing prog point is `%s` (%s), and the furthest prog point seen by you in this report is `%s` (%s).",
   			existingProgRole.Name,
   			existingProgRole.Phase(existingProgRoleIndex+1),
   			furthestProgRole.Name,
   			furthestProgRole.Phase(furthestProgRoleIndex+1),
   		))
   		fmt.Printf("BAILING: User has higher prog than report shows\n")
   		return false, messageString.String(), nil, nil
   	}
   	
   	// If this fight has the same furthest prog point the user already has,
   	// we are done.
   	if existingProgRole != nil && existingProgRoleIndex == furthestProgRoleIndex {
   		messageString.WriteString(fmt.Sprintf(
   			"Your furthest prog point, `%s` (%s), is the same as the furthest prog point in this report.",
   			existingProgRole.Name,
   			existingProgRole.Phase(existingProgRoleIndex+1),
   		))
   		fmt.Printf("BAILING: User already has same prog as report shows\n")
   		return false, messageString.String(), nil, nil
   	}
   	
   	// Looks like we have some real prog to give!
   	var lowerRoles []*Role
   	if furthestFight.Kill {
   		lowerRoles = roles.Roles
   	} else {
   		lowerRoles = roles.Roles[0:furthestProgRoleIndex]
   	}
   	
   	messageString.WriteString(fmt.Sprintf(
   		"Your furthest prog point is now `%s` (%s).\n",
   		furthestProgRole.Name,
   		furthestProgRole.Phase(furthestProgRoleIndex+1),
   	))
   	
   	fmt.Printf("SUCCESS: Should apply role %s\n", furthestProgRole.Name)
   	return true, messageString.String(), []*Role{furthestProgRole}, lowerRoles
   }
   return roles
}
