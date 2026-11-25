local initialization = {}

-- The client only exists to take screenshots; skip drawing what nobody sees.
local function trim_view(player)
  local view = player.game_view_settings
  view.show_entity_info = true
  view.show_minimap = false
  view.show_research_info = false
  view.show_side_menu = false
  view.show_map_view_options = false
  view.show_quickbar = false
  view.show_shortcut_bar = false
  view.show_controller_gui = false
  view.show_rail_block_visualisation = false
  view.update_entity_selection = false
end

local function prepare_game()
  game.forces.player.research_all_technologies()

  for _, player in pairs(game.players) do
    trim_view(player)
  end

  -- nauvis cannot be deleted; the player stays on it, so keep it one chunk.
  -- Only nauvis: preview surfaces are created with the default (unbounded) size.
  local nauvis = game.surfaces.nauvis
  if nauvis then
    local settings = nauvis.map_gen_settings
    settings.width = 64
    settings.height = 64
    nauvis.map_gen_settings = settings
  end

  for _, surface in pairs(game.surfaces) do
    game.delete_surface(surface)
  end

end

function initialization.on_init()
  prepare_game()
end

function initialization.on_configuration_changed()
  prepare_game()
end

function initialization.on_player_ready(event)
  local player = game.get_player(event.player_index)
  trim_view(player)
end

return initialization
