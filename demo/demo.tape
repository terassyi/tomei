Output demo.gif

Set FontSize 16
Set Width 1200
Set Height 600
Set WindowBar Colorful
Set Theme "Dracula"
Set TypingSpeed 60ms
Set Shell "bash"

# --- enter container ---
Type "docker exec -it tomei-demo bash"
Enter
Sleep 1s

# --- init ---
Type "tomei init --yes"
Enter
Sleep 3s

# --- clear ---
Type "clear"
Enter
Sleep 1s

# --- show manifests ---
Type "cat manifests/runtime.cue"
Enter
Sleep 3s

Type "cat manifests/tools.cue"
Enter
Sleep 3s

# --- clear ---
Type "clear"
Enter
Sleep 1s

# --- plan ---
Type "tomei plan manifests"
Enter
Sleep 5s

# --- apply ---
Type "tomei apply manifests --yes"
Enter
Wait+Line@300s /\$$/
Sleep 2s

# --- env + verify ---
Type 'eval "$(tomei env)"'
Enter
Sleep 2s

# --- go version ---
Type "go version"
Enter
Sleep 2s

# --- jq version ---
Type "jq --version"
Enter
Sleep 2s

# --- bat version ---
Type "bat --version"
Enter
Sleep 2s
