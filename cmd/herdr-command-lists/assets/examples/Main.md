#=========================== MAIN ===========================#

# Edit This List
nano "$(herdr plugin config-dir herdr.command-lists)/lists/Main.md"

#------------------- COMMAND LISTS PLUGIN -------------------#

# Plugin
nano ~/.config/herdr/config.toml
cat "$(herdr plugin config-dir herdr.command-lists)/HELP.md"
cd "$(herdr plugin config-dir herdr.command-lists)/lists"

#--------------------- EXAMPLE COMMANDS ---------------------#

# Examples
whoami
date

#---------------------- MORE EXAMPLES -----------------------#

# More
printf 'Hello World!\n'
cd "$HOME"
