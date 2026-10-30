# Account and OBS fixture views

Run the Vite development server and visit `/account?fixture=offline`. Other
fixture states are `unknown`, `missing_grant`, `capacity`, `unavailable`,
`not_listening`, `listening` and `paused`. The in-memory fixture implements
account actions without provider calls. `/obs?fixture=pairing` and
`/obs?fixture=paired` show the browser-source states. Fixture selection is
ignored by production builds.
