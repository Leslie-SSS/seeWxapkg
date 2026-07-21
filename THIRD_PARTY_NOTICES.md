# Third-party notices

SeeWxapkg is distributed under the MIT license except where a nested component
declares a different license.

The compatibility fallback under
`backend/internal/beautify/wxappUnpacker/` is derived from wxappUnpacker and is
licensed under GNU GPL version 3 or later (`GPL-3.0-or-later`). Its source,
local modifications and license text are shipped together in this repository.
The fallback runs as a separate Node.js process and its results are explicitly
labelled `fallback`. This notice does not determine the license status of input
packages or recovered artifacts; users are responsible for assessing their own
rights and compliance obligations.

Go modules and npm packages retain their respective upstream licenses. Their
exact resolved versions are recorded in `go.sum` and npm lock files.
