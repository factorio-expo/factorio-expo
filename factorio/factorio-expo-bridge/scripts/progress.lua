--- Job progress logging, one prefixed line per step so a job can be followed
--- through the Factorio log next to the runner's.
local progress = {}

function progress.log(job_id, message)
  log(string.format("Factorio Expo [%s] tick=%d %s", job_id, game.tick, message))
end

return progress
