let video = document.getElementById('video');

if(Hls.isSupported()) {
    let hls = new Hls();
    hls.loadSource('/playlist.m3u8');
    hls.attachMedia(video);
    hls.on(Hls.Events.MANIFEST_PARSED,function() {
        video.play();
    });
} else if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = '/playlist.m3u8';
    video.addEventListener('loadedmetadata',function() {
        video.play();
    });
}