%{
#include <libtorrent/alert_types.hpp>
%}

namespace libtorrent {
    class feed_handle;
    class feed_item;
    class stat;
}

%template(stdVectorChar) std::vector<char>;

%extend libtorrent::save_resume_data_alert {
    entry resume_data() const {
        return *self->resume_data;
    }
}
%ignore libtorrent::save_resume_data_alert::resume_data;

%ignore libtorrent::torrent_alert::torrent_alert;
%ignore libtorrent::peer_alert::peer_alert;
%ignore libtorrent::tracker_alert::tracker_alert;

%include "alert_types_mod.hpp"
