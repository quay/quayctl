%{
#include <libtorrent/peer_info.hpp>
%}

namespace libtorrent {
}

%extend libtorrent::peer_info {
    std::string ip() {
        return self->ip.address().to_string();
    }
    std::string local_endpoint() {
        return self->local_endpoint.address().to_string();
    }
}

%include <libtorrent/peer_info.hpp>
