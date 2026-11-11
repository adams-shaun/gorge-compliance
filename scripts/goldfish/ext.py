"""ext.py PAGE.html: print the decklist of a saved MTGGoldfish archetype page.

The list is the value of the page's deck_input[deck] field (one "N Card Name" a line; a blank line
before the sideboard, if any). Prints nothing when the field is missing. Used by fetch.sh.
"""
import html, re, sys

t = open(sys.argv[1], encoding='utf-8').read()
m = re.search(r'name="deck_input\[deck\]" id="deck_input_deck" value="([^"]*)"', t)
print(html.unescape(m.group(1)) if m else "")
