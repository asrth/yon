#!/usr/bin/env sh
# Inject the .yon document-type association into an already-built macOS .app
# bundle's Info.plist (CFBundleDocumentTypes + an exported UTI), so Finder opens
# double-clicked collections in Yon. Shared by package.sh and the release CI
# workflow so both produce a bundle with the association.
#
# Usage: sh packaging/macos/register-filetype.sh <path-to-.app>
set -eu

APP="${1:-Yon.app}"
UTI="th.co.asr.yon.collection"
PLIST="$APP/Contents/Info.plist"

if [ ! -f "$PLIST" ]; then
	echo "error: $PLIST not found (build the .app first)" >&2
	exit 1
fi

pb() { /usr/libexec/PlistBuddy -c "$1" "$PLIST"; }

# Declare that Yon opens documents of our UTI, and owns the file type.
pb "Add :CFBundleDocumentTypes array"
pb "Add :CFBundleDocumentTypes:0 dict"
pb "Add :CFBundleDocumentTypes:0:CFBundleTypeName string 'Yon Collection'"
pb "Add :CFBundleDocumentTypes:0:CFBundleTypeRole string Editor"
pb "Add :CFBundleDocumentTypes:0:LSHandlerRank string Owner"
pb "Add :CFBundleDocumentTypes:0:LSItemContentTypes array"
pb "Add :CFBundleDocumentTypes:0:LSItemContentTypes:0 string $UTI"

# Export the UTI: maps the .yon extension to our type (a kind of JSON). Also
# conform to public.content/public.data so Launch Services indexes it reliably.
pb "Add :UTExportedTypeDeclarations array"
pb "Add :UTExportedTypeDeclarations:0 dict"
pb "Add :UTExportedTypeDeclarations:0:UTTypeIdentifier string $UTI"
pb "Add :UTExportedTypeDeclarations:0:UTTypeDescription string 'Yon Collection'"
pb "Add :UTExportedTypeDeclarations:0:UTTypeConformsTo array"
pb "Add :UTExportedTypeDeclarations:0:UTTypeConformsTo:0 string public.json"
pb "Add :UTExportedTypeDeclarations:0:UTTypeConformsTo:1 string public.content"
pb "Add :UTExportedTypeDeclarations:0:UTTypeConformsTo:2 string public.data"
pb "Add :UTExportedTypeDeclarations:0:UTTypeTagSpecification dict"
pb "Add :UTExportedTypeDeclarations:0:UTTypeTagSpecification:public.filename-extension array"
pb "Add :UTExportedTypeDeclarations:0:UTTypeTagSpecification:public.filename-extension:0 string yon"

# Fail loudly if the edits produced an invalid plist.
plutil -lint "$PLIST"

echo "Registered the .yon file type in $PLIST"
