# backend/entrypoint.sh
#!/bin/sh
set -e

# exec your binary
exec ./main "$@"
