%{
#include <string.h>
#include <libtorrent/extensions/lt_trackers.hpp>
#include <libtorrent/extensions/smart_ban.hpp>
#include <libtorrent/extensions/ut_metadata.hpp>
#include <libtorrent/extensions/ut_pex.hpp>
%}

%extend libtorrent::session {
  void add_extensions() {
    self->add_extension(&libtorrent::create_lt_trackers_plugin);
    self->add_extension(&libtorrent::create_smart_ban_plugin);
    self->add_extension(&libtorrent::create_ut_metadata_plugin);
    self->add_extension(&libtorrent::create_ut_pex_plugin);
   }

   void add_extension_by_name(std::string extension_name) {
     if (extension_name.compare("lb_trackers") == 0) {
         self->add_extension(&libtorrent::create_lt_trackers_plugin);
     } else if (extension_name.compare("smart_ban") == 0) {
       self->add_extension(&libtorrent::create_smart_ban_plugin);
     } else if (extension_name.compare("ut_metadata") == 0) {
       self->add_extension(&libtorrent::create_ut_metadata_plugin);
     } else if (extension_name.compare("ut_pex") == 0) {
       self->add_extension(&libtorrent::create_ut_pex_plugin);
     }
   }
}
