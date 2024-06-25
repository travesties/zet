# Zet
*NOTE: This project is no longer being developed or maintained, as I have migrated to Obsidian vaults for note taking.*

zet is a cli utility to streamline the process of creating zettel ("slips") in my zettelkasten ("slipbox").

The original workflow:
* Generate a unique ID string (YYYYMMDDHHSS) for zettel titles to avoid name collisions.
* Manually create file structure that surrounds the new entry
  * ```
    YYYYMMDDHHSS/
    |  README.md
    ```
* Add title and contents to README.md file, being sure to prepend the ID string to the title.
* Add backlinks to any referenced entires.
* git add, commit, and push.

The zet workflow:
* Run the `zet` command, which opens a generated file with the ID string title within the above file structure.
* Close editor.
* Specify whether or not to automatically commit and push.
