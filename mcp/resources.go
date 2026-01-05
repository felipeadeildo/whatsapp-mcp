package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"whatsapp-mcp/paths"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources defines all MCP resources for documentation.
func (m *MCPServer) registerResources() {
	// cross-chat search guide
	m.server.AddResource(
		mcp.NewResource(
			"whatsapp://guide/cross-chat-search",
			"Finding Messages Across All Chats",
			mcp.WithResourceDescription("Comprehensive guide for finding all messages from a person across all WhatsApp conversations"),
			mcp.WithMIMEType("text/markdown"),
		),
		m.handleCrossChatSearchGuide,
	)

	// general workflows guide
	m.server.AddResource(
		mcp.NewResource(
			"whatsapp://guide/workflows",
			"WhatsApp MCP Workflow Guide",
			mcp.WithResourceDescription("Complete guide for common WhatsApp operations and workflows"),
			mcp.WithMIMEType("text/markdown"),
		),
		m.handleWorkflowGuide,
	)

	// JID format explanation
	m.server.AddResource(
		mcp.NewResource(
			"whatsapp://guide/jid-format",
			"WhatsApp JID Format Guide",
			mcp.WithResourceDescription("Understanding WhatsApp JIDs (identifiers) and how to use them"),
			mcp.WithMIMEType("text/markdown"),
		),
		m.handleJIDFormatGuide,
	)

	// search patterns documentation
	m.server.AddResource(
		mcp.NewResource(
			"whatsapp://guide/search-patterns",
			"Search Pattern Matching Guide",
			mcp.WithResourceDescription("Comprehensive guide for pattern matching, wildcards, and search techniques"),
			mcp.WithMIMEType("text/markdown"),
		),
		m.handleSearchPatternsGuide,
	)

	// dynamic media resource template
	m.server.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"whatsapp://media/{message_id}",
			"WhatsApp Media File",
			mcp.WithTemplateDescription("Access media file from a WhatsApp message (image, video, audio, document)"),
		),
		m.handleMediaResource,
	)
}

// handleCrossChatSearchGuide handles the cross-chat search guide resource request.
func (m *MCPServer) handleCrossChatSearchGuide(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	guide := `# Finding Messages Across All Chats

## Overview
The **most common and powerful use case** for WhatsApp MCP: finding ALL messages from a specific person across ALL your WhatsApp conversations (DMs, groups, channels, everywhere).

## Why This Matters
- **Context gathering**: Understand who someone is by seeing everything they've said
- **Relationship insights**: See patterns in communication across different contexts
- **Information retrieval**: Find important information someone mentioned anywhere
- **Complete picture**: Don't miss messages just because they're in a different chat

## The Critical Workflow

### Step 1: Get the Person's JID
Every WhatsApp user has a unique identifier (JID). You need this first.

**Tool**: ` + "`find_chat`" + `
**Command**: ` + "`find_chat(search=\"Arthur Kui\")`" + `
**Result**: Returns the chat with their JID, e.g., ` + "`558293093900@s.whatsapp.net`" + `

### Step 2: Search ALL Their Messages
Use ` + "`search_messages`" + ` with **ONLY** the ` + "`from`" + ` parameter.

**CRITICAL**: Do NOT include a ` + "`query`" + ` parameter - you want ALL their messages!

**Tool**: ` + "`search_messages`" + `
**Command**: ` + "`search_messages(from=\"558293093900@s.whatsapp.net\")`" + `
**Result**: ALL messages from Arthur across ALL chats

## Real-World Examples

### Example 1: Understanding a New Contact
**Scenario**: You met Arthur at a conference and want to understand who he is.

**Workflow**:
` + "```" + `
1. find_chat(search="Arthur")
   -> Returns: 558293093900@s.whatsapp.net

2. search_messages(from="558293093900@s.whatsapp.net")
   -> Returns: All messages from Arthur in:
      - Your DM with Arthur
      - Tech group where he posts
      - Conference planning group
      - Any other shared chats
` + "```" + `

**Result**: You see Arthur discusses tech topics, is interested in AI, and is organizing a meetup.

### Example 2: Finding Important Information
**Scenario**: Someone mentioned a restaurant name, but you can't remember where.

**Workflow**:
` + "```" + `
1. find_chat(search="Maria")
   -> Returns: 5511999999999@s.whatsapp.net

2. search_messages(from="5511999999999@s.whatsapp.net")
   -> Returns: All Maria's messages

3. Search through results for restaurant mentions
` + "```" + `

### Example 3: Analyzing Communication Patterns
**Scenario**: You want to understand how often Edeilson messages you.

**Workflow**:
` + "```" + `
1. find_chat(search="Edeilson")
   -> Returns: 558293093900@s.whatsapp.net

2. search_messages(from="558293093900@s.whatsapp.net", limit=200)
   -> Returns: Last 200 messages from Edeilson

3. Analyze timestamps, frequency, topics
` + "```" + `

## Advanced Usage

### Combining with Keyword Search
Want messages from someone about a specific topic?

**Command**: ` + "`search_messages(query=\"budget\", from=\"558293093900@s.whatsapp.net\")`" + `
**Result**: Only Arthur's messages that mention "budget"

### Date-Based Filtering
Find recent messages from someone:

**Command**: ` + "`search_messages(from=\"558293093900@s.whatsapp.net\", limit=50)`" + `
**Result**: Last 50 messages from Arthur

### Pagination
Get more messages:

` + "```" + `
# First batch
search_messages(from="558293093900@s.whatsapp.net", limit=100)

# Next batch
search_messages(from="558293093900@s.whatsapp.net", limit=100, offset=100)
` + "```" + `

## Common Mistakes

### ❌ Mistake 1: Including Query Parameter
**Wrong**: ` + "`search_messages(query=\"\", from=\"558293093900@s.whatsapp.net\")`" + `
**Right**: ` + "`search_messages(from=\"558293093900@s.whatsapp.net\")`" + `
**Why**: Empty query might be interpreted as "nothing", omit it entirely

### ❌ Mistake 2: Using get_chat_messages Instead
**Wrong**: ` + "`get_chat_messages(chat_jid=\"558293093900@s.whatsapp.net\")`" + `
**Why**: This only gets messages from YOUR DM with Arthur
**Right**: ` + "`search_messages(from=\"558293093900@s.whatsapp.net\")`" + `
**Why**: This gets messages from Arthur EVERYWHERE

### ❌ Mistake 3: Guessing the JID
**Wrong**: Constructing JID manually
**Right**: Always use ` + "`find_chat`" + ` first

## Performance Tips

1. **Limit results**: Use ` + "`limit`" + ` parameter for faster responses
2. **Specific searches**: Add ` + "`query`" + ` if you know what you're looking for
3. **Pagination**: Use ` + "`offset`" + ` for large result sets

## Quick Reference

**Most Common Pattern**:
` + "```" + `
find_chat(search="[name]") -> get JID
search_messages(from="[JID]") -> get ALL messages
` + "```" + `

**With Keyword**:
` + "```" + `
find_chat(search="[name]") -> get JID
search_messages(query="[keyword]", from="[JID]") -> get specific messages
` + "```" + `
`

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "whatsapp://guide/cross-chat-search",
			MIMEType: "text/markdown",
			Text:     guide,
		},
	}, nil
}

// handleWorkflowGuide handles the general workflow guide resource request.
func (m *MCPServer) handleWorkflowGuide(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	guide := `# WhatsApp MCP Workflow Guide

## Core Concept: JID-First Approach
Almost all operations require a **JID** (WhatsApp identifier). Always use ` + "`find_chat`" + ` first.

## Common Workflows

### 1. Send a Message
**Goal**: Send a WhatsApp message to someone.

**Steps**:
` + "```" + `
1. find_chat(search="contact name")
   -> Get chat_jid

2. send_message(chat_jid="[from step 1]", text="your message")
   -> Message sent
` + "```" + `

**Example**:
` + "```" + `
find_chat(search="Maria") -> 5511999999999@s.whatsapp.net
send_message(chat_jid="5511999999999@s.whatsapp.net", text="Hey Maria!")
` + "```" + `

### 2. Read Conversation History
**Goal**: See recent messages from a specific chat.

**Steps**:
` + "```" + `
1. find_chat(search="contact name")
   -> Get chat_jid

2. get_chat_messages(chat_jid="[from step 1]", limit=50)
   -> Get last 50 messages
` + "```" + `

**Example**:
` + "```" + `
find_chat(search="Tech Group") -> 120363123456789@g.us
get_chat_messages(chat_jid="120363123456789@g.us", limit=100)
` + "```" + `

### 3. Find All Messages from Someone (MOST COMMON)
**Goal**: See everything someone has ever said to you.

**Steps**:
` + "```" + `
1. find_chat(search="contact name")
   -> Get their JID

2. search_messages(from="[from step 1]")
   -> Get ALL their messages across ALL chats
` + "```" + `

**Example**:
` + "```" + `
find_chat(search="Arthur") -> 558293093900@s.whatsapp.net
search_messages(from="558293093900@s.whatsapp.net")
` + "```" + `

### 4. Search by Keyword
**Goal**: Find messages containing specific text.

**Steps**:
` + "```" + `
search_messages(query="budget meeting")
-> Returns all messages mentioning "budget meeting"
` + "```" + `

**Advanced**:
` + "```" + `
# Case-insensitive (default)
search_messages(query="budget")

# With wildcards (case-sensitive)
search_messages(query="*TODO*")

# From specific person
search_messages(query="budget", from="558293093900@s.whatsapp.net")
` + "```" + `

### 5. Browse All Chats
**Goal**: See all your conversations.

**Steps**:
` + "```" + `
list_chats(limit=50)
-> Returns 50 most recent chats
` + "```" + `

### 6. Load More History
**Goal**: Get older messages from WhatsApp servers.

**Steps**:
` + "```" + `
1. find_chat(search="contact name")
   -> Get chat_jid

2. load_more_messages(chat_jid="[from step 1]", count=100, wait_for_sync=true)
   -> Fetch 100 older messages

3. get_chat_messages(chat_jid="[from step 1]", limit=150)
   -> See the newly loaded messages
` + "```" + `

## Tool Selection Guide

### When to use find_chat
- Before ANY other operation (get JID first!)
- When you know the contact/group name
- When you need a JID for other tools

### When to use get_chat_messages
- Reading messages from ONE specific chat
- Browsing conversation history chronologically
- Getting messages in order (most recent first)

### When to use search_messages
- Finding messages from someone across ALL chats
- Searching by keyword/content
- Cross-chat queries
- Getting context about someone

### When to use list_chats
- Browsing all your conversations
- Getting an overview of recent activity
- Finding multiple chat JIDs at once

### When to use send_message
- Sending a WhatsApp message
- (Always use find_chat first to get JID!)

### When to use load_more_messages
- Need older messages not yet in database
- Building complete conversation history
- Accessing historical data

## Best Practices

### 1. Always Get JIDs from find_chat
**Never** manually construct JIDs. Always use ` + "`find_chat`" + ` first.

**Why**: JID formats can be complex and vary (phone numbers, group IDs, etc.)

### 2. Use Appropriate Limits
Start with reasonable limits (50-100) to avoid overwhelming results.

### 3. Understand Tool Scope
- ` + "`get_chat_messages`" + `: ONE chat only
- ` + "`search_messages`" + `: ALL chats

### 4. Pagination for Large Results
Use ` + "`offset`" + ` or timestamp-based pagination for large datasets.

### 5. Check Timezone Settings
Timestamps are shown in server timezone (` + m.timezone.String() + `).

## Common Patterns

### Pattern 1: Person Analysis
` + "```" + `
find_chat -> search_messages(from=JID) -> analyze content
` + "```" + `

### Pattern 2: Conversation Summary
` + "```" + `
find_chat -> get_chat_messages -> summarize
` + "```" + `

### Pattern 3: Information Retrieval
` + "```" + `
search_messages(query=keyword) -> review results
` + "```" + `

### Pattern 4: Messaging
` + "```" + `
find_chat -> send_message
` + "```" + `

## Troubleshooting

### "Chat not found"
- Check spelling in ` + "`find_chat`" + `
- Try partial names or nicknames
- Use wildcards: ` + "`find_chat(search=\"*Maria*\")`" + `

### "No messages returned"
- Verify JID is correct
- Check if history is loaded: use ` + "`load_more_messages`" + `
- Verify search parameters

### "Too many results"
- Add ` + "`limit`" + ` parameter
- Use more specific ` + "`query`" + `
- Add date filters
`

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "whatsapp://guide/workflows",
			MIMEType: "text/markdown",
			Text:     guide,
		},
	}, nil
}

// handleJIDFormatGuide handles the JID format guide resource request.
func (m *MCPServer) handleJIDFormatGuide(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	guide := `# WhatsApp JID Format Guide

## What is a JID?
JID (Jabber ID) is WhatsApp's unique identifier for every user, group, and chat.

**Think of it as**: WhatsApp's version of an email address or user ID.

## Why JIDs Matter
- **Required for operations**: Most tools need a JID (chat_jid parameter)
- **Unique identifiers**: Same person = same JID (unlike names which can duplicate)
- **Cross-platform**: Works across all WhatsApp clients

## JID Formats

### 1. Direct Messages (DMs)
**Format**: ` + "`[phone_number]@s.whatsapp.net`" + `

**Examples**:
- ` + "`5511999999999@s.whatsapp.net`" + ` (Brazil)
- ` + "`12125551234@s.whatsapp.net`" + ` (USA)
- ` + "`447700900123@s.whatsapp.net`" + ` (UK)

**Pattern**: Country code + phone number (no spaces, no + sign)

### 2. Group Chats
**Format**: ` + "`[group_id]@g.us`" + `

**Examples**:
- ` + "`120363123456789@g.us`" + `
- ` + "`120363198765432@g.us`" + `

**Pattern**: Numeric group ID + @g.us suffix

### 3. Channels (if supported)
**Format**: ` + "`[channel_id]@newsletter`" + `

## How to Get JIDs

### Method 1: find_chat (RECOMMENDED)
Always use ` + "`find_chat`" + ` to get JIDs.

**Example**:
` + "```" + `
find_chat(search="Maria Silva")
-> Returns: chat with JID 5511999999999@s.whatsapp.net
` + "```" + `

### Method 2: list_chats
Get multiple JIDs at once.

**Example**:
` + "```" + `
list_chats(limit=50)
-> Returns: List of all chats with their JIDs
` + "```" + `

### ❌ Never Do This
**Don't manually construct JIDs!**

**Wrong**: Guessing ` + "`5511999999999@s.whatsapp.net`" + ` from a phone number
**Why**:
- Phone numbers might not be registered on WhatsApp
- Special cases exist (business accounts, etc.)
- Typos cause failures

**Right**: Use ` + "`find_chat`" + ` first

## Using JIDs

### In Tool Parameters
Most tools accept ` + "`chat_jid`" + ` parameter:

` + "```" + `
get_chat_messages(chat_jid="5511999999999@s.whatsapp.net")
send_message(chat_jid="5511999999999@s.whatsapp.net", text="Hello")
load_more_messages(chat_jid="5511999999999@s.whatsapp.net")
` + "```" + `

### In Search Filters
Use ` + "`from`" + ` parameter for sender filtering:

` + "```" + `
search_messages(from="5511999999999@s.whatsapp.net")
get_chat_messages(chat_jid="120363123456789@g.us", from="5511999999999@s.whatsapp.net")
` + "```" + `

## JID vs. Name

### Names
- **Human-readable**: "Maria Silva", "Tech Group"
- **Can change**: Users can change display names
- **Can duplicate**: Multiple "Maria"s exist
- **Use for**: ` + "`find_chat`" + ` searches

### JIDs
- **Machine-readable**: "5511999999999@s.whatsapp.net"
- **Never change**: Permanent identifier
- **Always unique**: One person = one JID
- **Use for**: All other operations

## Real-World Examples

### Example 1: Simple Workflow
` + "```" + `
# Step 1: Find by name
find_chat(search="Maria")
-> Result: { name: "Maria Silva", jid: "5511999999999@s.whatsapp.net" }

# Step 2: Use JID for operations
get_chat_messages(chat_jid="5511999999999@s.whatsapp.net")
` + "```" + `

### Example 2: Group Chat
` + "```" + `
# Step 1: Find group
find_chat(search="Tech Team")
-> Result: { name: "Tech Team 💻", jid: "120363123456789@g.us" }

# Step 2: Get messages from specific person in group
get_chat_messages(
  chat_jid="120363123456789@g.us",
  from="5511999999999@s.whatsapp.net"
)
` + "```" + `

### Example 3: Cross-Chat Search
` + "```" + `
# Find ALL messages from Maria (across all chats)
find_chat(search="Maria") -> 5511999999999@s.whatsapp.net
search_messages(from="5511999999999@s.whatsapp.net")

# This searches:
# - Your DM with Maria
# - Tech Team group where Maria posts
# - Family group where Maria posts
# - ANY chat where Maria sent messages
` + "```" + `

## Common Issues

### Issue 1: "Invalid JID"
**Cause**: Malformed JID string
**Solution**: Use ` + "`find_chat`" + ` instead of constructing manually

### Issue 2: "Chat not found"
**Cause**: JID doesn't exist in your contacts
**Solution**: Verify with ` + "`find_chat`" + ` or ` + "`list_chats`" + `

### Issue 3: "Group vs DM confusion"
**Cause**: Used wrong JID format
**Solution**:
- DMs end with ` + "`@s.whatsapp.net`" + `
- Groups end with ` + "`@g.us`" + `

## Quick Reference

**Workflow**: Name -> JID -> Operations

**Pattern**:
` + "```" + `
find_chat(search="[name]") -> get JID
[any_tool](chat_jid="[JID]") -> perform operation
` + "```" + `

**Remember**: JIDs are permanent, names are not!
`

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "whatsapp://guide/jid-format",
			MIMEType: "text/markdown",
			Text:     guide,
		},
	}, nil
}

// handleSearchPatternsGuide handles the search patterns guide resource request.
func (m *MCPServer) handleSearchPatternsGuide(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	guide := `# Search Pattern Matching Guide

## Overview
WhatsApp MCP supports powerful pattern matching for searching chats and messages.

## Default Behavior: Case-Insensitive Substring

### Basic Search
By default, searches are **case-insensitive** and match **substrings**.

**Examples**:
` + "```" + `
find_chat(search="maria")
-> Matches: "Maria Silva", "MARIA", "maria", "Rosemaria"

search_messages(query="meeting")
-> Matches: "Meeting tomorrow", "budget meeting", "MEETING NOTES"
` + "```" + `

**How it works**: Pattern is converted to lowercase and matched anywhere in text.

## Wildcards: Advanced Matching

### Wildcard Characters
When you use wildcards, matching becomes **case-sensitive**.

#### Asterisk (*) - Any Characters
Matches **zero or more** characters.

**Examples**:
` + "```" + `
# Match "Maria" at start
find_chat(search="Maria*")
-> Matches: "Maria Silva", "Maria123"
-> Doesn't match: "maria silva" (case-sensitive!)

# Match "Group" anywhere
find_chat(search="*Group*")
-> Matches: "Tech Group", "GROUP CHAT", "My Group"

# Match "TODO" anywhere (case-sensitive)
search_messages(query="*TODO*")
-> Matches: "TODO: fix bug", "Remember TODO"
-> Doesn't match: "todo: fix bug"
` + "```" + `

#### Question Mark (?) - Single Character
Matches **exactly one** character.

**Examples**:
` + "```" + `
# Match dates
search_messages(query="2024-??-31")
-> Matches: "2024-01-31", "2024-12-31"

# Match variations
find_chat(search="Mar?a")
-> Matches: "Maria", "Marla", "Marta"
` + "```" + `

### Character Classes: [...]

#### Basic Character Class
Match **one character** from a set.

**Syntax**: ` + "`[abc]`" + ` matches 'a', 'b', or 'c'

**Examples**:
` + "```" + `
# Match "color" or "colour"
search_messages(query="colo[u]?r")
-> Matches: "color", "colour"

# Match variations
search_messages(query="[Hh]ello")
-> Matches: "Hello", "hello"
-> Doesn't match: "HELLO"
` + "```" + `

#### Character Ranges
Use hyphen for ranges.

**Examples**:
` + "```" + `
# Match any digit
search_messages(query="Version [0-9]")
-> Matches: "Version 1", "Version 9"

# Match letters
find_chat(search="Team [A-Z]")
-> Matches: "Team A", "Team B"
` + "```" + `

#### Negation: [^...]
Match any character **except** those listed.

**Examples**:
` + "```" + `
# Match non-digits
search_messages(query="ID[^0-9]*")
-> Matches: "IDABC", "IDxyz"
-> Doesn't match: "ID123"
` + "```" + `

## Real-World Examples

### Example 1: Finding Variations
**Goal**: Find "TODO", "ToDo", "todo" (case variations)

**Solutions**:
` + "```" + `
# Option 1: Case-insensitive (no wildcards)
search_messages(query="todo")
-> Matches all variations

# Option 2: Explicit pattern
search_messages(query="[Tt][Oo][Dd][Oo]")
-> Matches: "TODO", "todo", "ToDo", "tOdO"
` + "```" + `

### Example 2: Date Patterns
**Goal**: Find all December 2024 dates

**Solution**:
` + "```" + `
search_messages(query="2024-12-*")
-> Matches: "2024-12-01", "2024-12-31"
` + "```" + `

### Example 3: Phone Numbers
**Goal**: Find Brazilian mobile numbers (+55 11 9XXXX-XXXX)

**Solution**:
` + "```" + `
search_messages(query="*55*11*9*")
-> Matches messages containing numbers like "+55 11 98765-4321"
` + "```" + `

### Example 4: Exact Phrase (Case-Sensitive)
**Goal**: Find exact "TODO:" (uppercase only)

**Solution**:
` + "```" + `
search_messages(query="*TODO:*")
-> Matches: "TODO: fix bug"
-> Doesn't match: "todo: fix bug"
` + "```" + `

### Example 5: Person Names
**Goal**: Find chats with "João" (including special characters)

**Solution**:
` + "```" + `
find_chat(search="joão")
-> Matches: "João Silva", "JOÃO SANTOS"

# Or for exact case:
find_chat(search="João*")
-> Matches: "João Silva"
-> Doesn't match: "joão silva"
` + "```" + `

## Performance Tips

### 1. Be Specific
**Slow**: ` + "`search_messages(query=\"*\")`" + ` (matches everything)
**Fast**: ` + "`search_messages(query=\"budget meeting\")`" + `

### 2. Use Limits
**Example**:
` + "```" + `
search_messages(query="todo", limit=50)
` + "```" + `

### 3. Combine with Filters
**Example**:
` + "```" + `
# Search only in messages from Maria
search_messages(query="budget", from="5511999999999@s.whatsapp.net")
` + "```" + `

### 4. Start Specific, Then Broaden
**Approach**:
` + "```" + `
# Try 1: Exact phrase
search_messages(query="quarterly budget report")

# Try 2: Broader
search_messages(query="budget report")

# Try 3: Even broader
search_messages(query="budget")
` + "```" + `

## Common Patterns Cheat Sheet

| Goal | Pattern | Example |
|------|---------|---------|
| Case-insensitive | No wildcards | ` + "`search=\"maria\"`" + ` |
| Starts with | ` + "`Pattern*`" + ` | ` + "`search=\"Maria*\"`" + ` |
| Ends with | ` + "`*Pattern`" + ` | ` + "`search=\"*Silva\"`" + ` |
| Contains | ` + "`*Pattern*`" + ` | ` + "`search=\"*Group*\"`" + ` |
| Exact match | ` + "`Pattern`" + ` (no wildcards) | ` + "`search=\"Maria Silva\"`" + ` |
| One character | ` + "`?`" + ` | ` + "`search=\"Mar?a\"`" + ` |
| Character set | ` + "`[abc]`" + ` | ` + "`search=\"[Tt]ech\"`" + ` |
| Range | ` + "`[a-z]`" + ` | ` + "`search=\"Team[A-Z]\"`" + ` |
| Not in set | ` + "`[^abc]`" + ` | ` + "`search=\"ID[^0-9]\"`" + ` |

## Case Sensitivity Rules

### When is it Case-Insensitive?
- **No wildcards**: ` + "`search=\"maria\"`" + ` -> matches "Maria", "MARIA"

### When is it Case-Sensitive?
- **Any wildcard**: ` + "`search=\"Maria*\"`" + ` -> only matches "Maria...", not "maria..."
- **Character classes**: ` + "`search=\"[Mm]aria\"`" + ` -> matches "Maria" or "maria"

## Troubleshooting

### "No results found"
**Check**:
1. Are you using wildcards? (they're case-sensitive)
2. Try simpler pattern: remove wildcards
3. Try case-insensitive: remove wildcards and lowercase

### "Too many results"
**Solutions**:
1. Add ` + "`limit`" + ` parameter
2. Be more specific in pattern
3. Add additional filters (` + "`from`" + `, date range)

### "Pattern not working as expected"
**Remember**:
- Wildcards make search case-sensitive
- ` + "`*`" + ` matches any characters (including none)
- ` + "`?`" + ` matches exactly one character
- ` + "`[...]`" + ` matches one character from set

## Quick Reference

**Default** (case-insensitive):
` + "```" + `
search_messages(query="todo")
` + "```" + `

**Case-sensitive** (with wildcards):
` + "```" + `
search_messages(query="*TODO*")
` + "```" + `

**Combined**:
` + "```" + `
search_messages(query="budget*", from="558293093900@s.whatsapp.net", limit=50)
` + "```" + `
`

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "whatsapp://guide/search-patterns",
			MIMEType: "text/markdown",
			Text:     guide,
		},
	}, nil
}

// handleMediaResource handles dynamic media resource requests.
func (m *MCPServer) handleMediaResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	uri := req.Params.URI

	var messageID string
	if req.Params.Arguments != nil {
		if messageIDs, ok := req.Params.Arguments["message_id"].([]string); ok && len(messageIDs) > 0 {
			messageID = messageIDs[0]
		}
	}

	if messageID == "" {
		return nil, errors.New("invalid message id")
	}

	// get media metadata
	meta, err := m.mediaStore.GetMediaMetadata(messageID)
	if err != nil || meta == nil {
		return nil, fmt.Errorf("media not found for message: %s", messageID)
	}

	// check download status
	if meta.DownloadStatus != "downloaded" {
		return nil, fmt.Errorf("media not downloaded (status: %s). Enable auto-download or download manually.", meta.DownloadStatus)
	}

	// sanitize and validate file path to prevent directory traversal
	cleanPath := filepath.Clean(meta.FilePath)
	if strings.Contains(cleanPath, "..") {
		return nil, errors.New("invalid file path: path traversal detected")
	}

	// construct full file path
	fullPath := paths.GetMediaPath(cleanPath)

	// get absolute paths for security validation
	mediaDir, err := filepath.Abs(paths.DataMediaDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve media directory: %w", err)
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve file path: %w", err)
	}

	// ensure the resolved path is within the media directory
	if !strings.HasPrefix(absPath, mediaDir+string(filepath.Separator)) && absPath != mediaDir {
		return nil, errors.New("invalid file path: outside media directory")
	}

	// verify file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		m.log.Printf("Media file not found at path: %s", absPath)
		return nil, errors.New("media file not found")
	}

	// read the actual file data (use validated absolute path)
	fileData, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read media file: %w", err)
	}

	// encode to base64 for transmission
	encodedData := base64.StdEncoding.EncodeToString(fileData)

	// return the file as a blob so AI assistants can view it
	return []mcp.ResourceContents{
		mcp.BlobResourceContents{
			URI:      uri,
			MIMEType: meta.MimeType,
			Blob:     encodedData,
		},
	}, nil
}
