%{
#include <libtorrent/torrent_handle.hpp>
%}

%include <std_vector.i>
%include <std_pair.i>
%include <carrays.i>

%template(stdVectorPartialPieceInfo) std::vector<libtorrent::partial_piece_info>;
%template(stdVectorAnnounceEntry) std::vector<libtorrent::announce_entry>;
%template(stdVectorPeerInfo) std::vector<libtorrent::peer_info>;
%template(stdVectorInt) std::vector<int>;
%template(stdVectorSizeType) std::vector<long long>;
%template(stdPairIntInt) std::pair<int, int>;
%template(stdPairStringInt) std::pair<std::string, int>;

// Equaler interface
%rename(Equal) libtorrent::torrent_handle::operator==;
%rename(NotEqual) libtorrent::torrent_handle::operator!=;
%rename(Less) libtorrent::torrent_handle::operator<;

%array_class(libtorrent::block_info, block_info_list);

// Since the refcounter is allocated with libtorrent_info,
// we can just increase the refcount and return the raw pointer.
// Once we delete the object, it will also delete the refcounter.
%extend libtorrent::torrent_handle {
    const libtorrent::torrent_info* torrent_file() {
        return self->torrent_file().detach();
    }
}
%ignore libtorrent::torrent_handle::torrent_file;

%extend libtorrent::partial_piece_info {
    block_info_list* blocks() {
        return block_info_list_frompointer(self->blocks);
    }
}
%ignore libtorrent::partial_piece_info::blocks;

%include <libtorrent/torrent_handle.hpp>
