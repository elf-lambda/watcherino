"use strict";

let currentChannel = "";
let channels = [];
let connectedChannels = new Set(); // Track which channels are connected
let maxMessages = 100;
let messageElements = [];
let autoScrollEnabled = true;
let isRendering = false;

const chatMessages = document.getElementById("chat-messages");
const channelList = document.getElementById("channel-list");
const activeChannelEl = document.getElementById("active-channel");
const connectionStatus = document.getElementById("connection-status");
const loadingOverlay = document.getElementById("loading-overlay");
const customChannelInput = document.getElementById("custom-channel");
const connectCustomBtn = document.getElementById("connect-custom");
const newChannelInput = document.getElementById("new-channel");
const addChannelBtn = document.getElementById("add-channel");
const disconnectBtn = document.getElementById("disconnect-btn");
const viewerCountEl = document.getElementById("viewer-count");
const connectBtn = document.getElementById("connect-btn");

const minimizeBtn = document.getElementById("minimize-btn");
const restoreBtn = document.getElementById("restore-btn");
const channelPanel = document.getElementById("channel-panel");
const appContainer = document.querySelector(".app-container");

const audioLockBtn = document.getElementById("audio-lock-btn");
let audioLocked = false;
const audioToggleBtn = document.getElementById("audio-toggle-btn");
let audioMuted = false;

document.addEventListener("DOMContentLoaded", async () => {
    setupEventListeners();
    await loadChannels();
    await loadBufferSize();
    setupWailsEventListeners();
    updateButtonVisibility();
});

// Load buffer size from backend
async function loadBufferSize() {
    try {
        maxMessages = await go.main.App.GetBufferSize();
        console.log(`Message buffer size set to: ${maxMessages}`);
    } catch (error) {
        console.error("Failed to load buffer size:", error);
    }
}

// Load channels from backend
async function loadChannels() {
    try {
        channels = await go.main.App.GetChannels();
        console.log("Loaded channels:", channels);
        await renderChannelList();
        updateButtonVisibility();
    } catch (error) {
        console.error("Failed to load channels:", error);
        showError("Failed to load channels");
    }
}

// Util
function isAtBottom() {
    return (
        chatMessages.scrollHeight - chatMessages.scrollTop <=
        chatMessages.clientHeight + 100
    );
}

// Client-side message sending via websockets
async function sendMessageClientSide(message) {
    if (!currentChannel || !message) return;

    try {
        const twitchConfig = await go.main.App.GetTwitchConfig();

        return new Promise((resolve, reject) => {
            const ws = new WebSocket("wss://irc-ws.chat.twitch.tv:443");
            let authenticated = false;
            let messageSent = false;

            const timeout = setTimeout(() => {
                ws.close();
                reject(new Error("Connection timeout"));
            }, 10000);

            ws.onopen = () => {
                console.log("Connected to Twitch IRC");
                ws.send(`PASS ${twitchConfig.oauthToken}`);
                ws.send(`NICK ${twitchConfig.nickname}`);
                ws.send(`JOIN ${currentChannel}`);
            };

            ws.onmessage = (event) => {
                const data = event.data.trim();
                console.log("IRC:", data);

                if (data.startsWith("PING")) {
                    ws.send(`PONG ${data.substring(5)}`);
                    return;
                }

                if (data.includes("366") || data.includes("JOIN")) {
                    if (!messageSent) {
                        messageSent = true;
                        ws.send(`PRIVMSG ${currentChannel} :${message}`);

                        setTimeout(() => {
                            ws.close();
                            clearTimeout(timeout);
                            resolve();
                        }, 500);
                    }
                }
            };

            ws.onerror = (error) => {
                console.error("WebSocket error:", error);
                clearTimeout(timeout);
                reject(error);
            };

            ws.onclose = () => {
                console.log("Disconnected from Twitch IRC");
                clearTimeout(timeout);
                if (!messageSent) {
                    reject(new Error("Connection closed before message sent"));
                }
            };
        });
    } catch (error) {
        console.error("Failed to send message:", error);
        showError(`Failed to send message: ${error.message}`);
    }
}

// Setup event listeners for UI elements
function setupEventListeners() {
    if (minimizeBtn) {
        minimizeBtn.addEventListener("click", () => {
            console.log("Minimize clicked"); // Debug log
            channelPanel.style.display = "none";
            restoreBtn.style.display = "block";
            appContainer.classList.add("panel-hidden");
            minimizeBtn.style = "display: none";
        });
    } else {
        console.log("minimizeBtn not found");
    }

    if (restoreBtn) {
        restoreBtn.addEventListener("click", () => {
            console.log("Restore clicked");
            channelPanel.style.display = "flex";
            restoreBtn.style.display = "none";
            appContainer.classList.remove("panel-hidden");
            minimizeBtn.style = "display: show";
            scrollToBottom();
        });
    } else {
        console.log("restoreBtn not found");
    }

    if (connectBtn) {
        connectBtn.addEventListener("click", async () => {
            showLoading(true);
            try {
                await go.main.App.ConnectToAllChannels();
                addSystemMessage("Connecting to all channels...");
            } catch (error) {
                console.error("Failed to connect to all channels:", error);
                showError("Failed to connect to all channels");
            } finally {
                showLoading(false);
            }
        });
    }

    // Send message client side
    if (connectCustomBtn) {
        connectCustomBtn.addEventListener("click", async () => {
            const message = customChannelInput.value.trim();
            if (message && currentChannel) {
                await sendMessageClientSide(message);
                customChannelInput.value = "";
            }
        });
    }

    if (customChannelInput) {
        customChannelInput.addEventListener("keypress", async (e) => {
            if (e.key === "Enter") {
                const message = customChannelInput.value.trim();
                if (message && currentChannel) {
                    await sendMessageClientSide(message);
                    customChannelInput.value = "";
                }
            }
        });
    }

    // Add new channel
    if (addChannelBtn) {
        addChannelBtn.addEventListener("click", async () => {
            const channel = newChannelInput.value.trim();
            if (channel) {
                await addChannel(channel);
                newChannelInput.value = "";
            }
        });
    }

    if (newChannelInput) {
        newChannelInput.addEventListener("keypress", async (e) => {
            if (e.key === "Enter") {
                const channel = newChannelInput.value.trim();
                if (channel) {
                    await addChannel(channel);
                    newChannelInput.value = "";
                }
            }
        });
    }

    // Disconnect all button
    if (disconnectBtn) {
        disconnectBtn.addEventListener("click", async () => {
            await disconnectAllChannels();
        });
    }

    if (audioLockBtn) {
        audioLockBtn.addEventListener("click", async () => {
            try {
                audioLocked = !audioLocked;
                audioLockBtn.textContent = audioLocked ? "🔒" : "🔓";
                audioLockBtn.classList.toggle("locked", audioLocked);
                await go.main.App.SetAudioLock(audioLocked);
            } catch (error) {
                console.error("Failed to lock audio:", error);
            }
        });
    }

    if (audioToggleBtn) {
        audioToggleBtn.addEventListener("click", async () => {
            try {
                audioMuted = await go.main.App.ToggleAudioMute();
                audioToggleBtn.textContent = audioMuted ? "🔇" : "🔊";
                audioToggleBtn.classList.toggle("muted", audioMuted);
            } catch (error) {
                console.error("Failed to toggle audio:", error);
            }
        });
    }
}

// Update button visibility based on connection status
function updateButtonVisibility() {
    const hasConnectedChannels = connectedChannels.size > 0;

    if (connectBtn && disconnectBtn) {
        if (hasConnectedChannels) {
            connectBtn.style.display = "none";
            disconnectBtn.style.display = "inline-block";
        } else {
            connectBtn.style.display = "inline-block";
            disconnectBtn.style.display = "none";
        }
    }
}

// Highlight chat message message
function highlightMessage(message) {
    if (
        currentChannel === message.channel ||
        currentChannel === "#" + message.channel
    ) {
        const messages = chatMessages.querySelectorAll(".chat-message");
        if (messages.length > 0) {
            const lastMessage = messages[messages.length - 1];
            lastMessage.classList.add("message-highlighted");
        }
    }
}

// Highlight channel even if its not selected
function highlightChannel(channelName) {
    const normalizedChannel = channelName.startsWith("#")
        ? channelName.replace("#", "")
        : channelName;

    const channelItems = document.querySelectorAll(".channel-item");

    channelItems.forEach((item) => {
        const nameEl = item.querySelector(".channel-name");
        if (nameEl && nameEl.textContent === normalizedChannel) {
            item.classList.add("channel-highlighted");
        }
    });
}

// Setup Wails event listeners
function setupWailsEventListeners() {
    // Listen for new messages (only for active channel now)
    runtime.EventsOn("new-message", (message) => {
        addMessageToChat(message);
    });

    runtime.EventsOn("highlight-channel", (message) => {
        highlightChannel(message.channel);
    });

    // Listen for channel messages when switching
    runtime.EventsOn("channel-messages", (data) => {
        clearChatMessages();
        if (data.messages && data.messages.length > 0) {
            data.messages.forEach((message) =>
                addMessageToChat(message, false)
            );
        }
        scrollToBottom();
    });

    // Listen for channel switch
    runtime.EventsOn("channel-switched", (channel) => {
        updateActiveChannel(channel);
        addSystemMessage(`Switched to ${channel}`);
    });

    // Listen for channel connected
    runtime.EventsOn("channel-connected", async (channel) => {
        connectedChannels.add(channel);
        updateActiveChannel(channel);
        await renderChannelList();
        updateButtonVisibility();
        addSystemMessage(`Connected to ${channel}`);
    });

    // Listen for channel disconnected
    runtime.EventsOn("channel-disconnected", async (channel) => {
        console.log(`Channel disconnected event: ${channel}`);
        connectedChannels.delete(channel);
        await renderChannelList();
        updateButtonVisibility();
        addSystemMessage(`Disconnected from ${channel}`);
    });

    // Listen for channel removed (separate from disconnected)
    runtime.EventsOn("channel-removed", (channel) => {
        console.log(`Channel removed event: ${channel}`);
        connectedChannels.delete(
            channel.startsWith("#") ? channel : "#" + channel
        );
        loadChannels();
        updateButtonVisibility();
        addSystemMessage(`Removed ${channel}`);
    });

    // Listen for active channel disconnected
    runtime.EventsOn("active-channel-disconnected", (channel) => {
        console.log(`Active channel disconnected event: ${channel}`);
        currentChannel = "";
        if (activeChannelEl)
            activeChannelEl.textContent = "No channel selected";
        clearChatMessages();
        updateConnectionStatus(false);
    });

    // Listen for all channels disconnected
    runtime.EventsOn("all-channels-disconnected", async () => {
        console.log("All channels disconnected event");
        connectedChannels.clear();
        currentChannel = "";
        if (activeChannelEl)
            activeChannelEl.textContent = "No channel selected";
        clearChatMessages();
        updateConnectionStatus(false);
        await renderChannelList();
        updateButtonVisibility();
    });

    // Listen for reward redemptions
    runtime.EventsOn("reward-redemption", (reward) => {
        addRewardToChat(reward);
    });

    // Listen for connection errors
    runtime.EventsOn("connection-error", (data) => {
        showError(`Connection error for ${data.channel}: ${data.error}`);
    });

    // Listen for viewer count updates
    runtime.EventsOn("viewer-count", (count) => {
        const viewerCountNumber = document.getElementById(
            "viewer-count-number"
        );
        if (viewerCountNumber) {
            viewerCountNumber.textContent = count.toLocaleString();
        }
        if (viewerCountEl) {
            viewerCountEl.style.display = "inline";
        }
    });

    // Listen for channel live status updates
    runtime.EventsOn("channel-live-status", (data) => {
        console.log("Received live status update:", data);
        updateChannelLiveStatus(data.channel, data.isLive);
        sortChannelList();
    });
}

// Switch to channel (connect if needed)
async function switchToChannel(channel) {
    if (!channel) return;

    showLoading(true);

    try {
        await go.main.App.SwitchToChannel(channel);

        const normalizedChannel = channel.startsWith("#")
            ? channel
            : "#" + channel;
        currentChannel = normalizedChannel;

        updateConnectionStatus(true);
        updateButtonVisibility();

        // Get initial viewer count
        try {
            const initialCount = await go.main.App.GetViewerCount(channel);
            if (initialCount > 0) {
                const viewerCountNumber = document.getElementById(
                    "viewer-count-number"
                );
                if (viewerCountNumber) {
                    viewerCountNumber.textContent =
                        initialCount.toLocaleString();
                }
                if (viewerCountEl) {
                    viewerCountEl.style.display = "inline";
                }
            }
        } catch (e) {
            console.log("Couldn't get initial viewer count", e);
        }

        await renderChannelList();
    } catch (error) {
        console.error("Failed to switch to channel:", error);
        showError(`Failed to switch to ${channel}: ${error}`);
        updateConnectionStatus(false);
    } finally {
        showLoading(false);
    }
}

// Disconnect from all channels
async function disconnectAllChannels() {
    try {
        await go.main.App.DisconnectFromAllChannels();
        addSystemMessage("Disconnected from all channels");
    } catch (error) {
        console.error("Failed to disconnect from all channels:", error);
        showError("Failed to disconnect from all channels");
    }
}

// Add a new channel
async function addChannel(channel) {
    if (!channel) return;

    try {
        await go.main.App.AddChannel(channel);
        await new Promise((r) => setTimeout(r, 50));
        await loadChannels();
    } catch (error) {
        console.error("Failed to add channel:", error);
        showError("Failed to add channel");
    }
}

// Remove a channel
async function removeChannel(channel) {
    try {
        await go.main.App.RemoveChannel(channel);
        await loadChannels(); // Refresh the list
    } catch (error) {
        console.error("Failed to remove channel:", error);
        showError("Failed to remove channel");
    }
}

// Render channel list with connection status
async function renderChannelList() {
    if (isRendering) return;
    isRendering = true;
    try {
        console.log("Rendering channel list with channels:", channels);
        if (!channelList) return;

        channelList.innerHTML = "";

        if (channels.length === 0) {
            const emptyMessage = document.createElement("div");
            emptyMessage.className = "empty-channels";
            emptyMessage.textContent = "No channels configured";
            channelList.appendChild(emptyMessage);
            return;
        }

        const channelStatuses = [];
        for (const channel of channels) {
            const dataChannelName = channel.startsWith("#")
                ? channel
                : "#" + channel;
            try {
                const isLive = await go.main.App.GetChannelLiveStatus(channel);
                const isConnected = connectedChannels.has(dataChannelName);

                channelStatuses.push({
                    name: channel,
                    dataName: dataChannelName,
                    isLive: isLive,
                    isConnected: isConnected,
                });
            } catch (error) {
                console.error(
                    `Failed to get live status for ${channel}:`,
                    error
                );
                channelStatuses.push({
                    name: channel,
                    dataName: dataChannelName,
                    isLive: false,
                    isConnected: connectedChannels.has(dataChannelName),
                });
            }
        }

        // Sort connected first, then live, then alphabetically
        channelStatuses.sort((a, b) => {
            if (a.isConnected && !b.isConnected) return -1;
            if (!a.isConnected && b.isConnected) return 1;
            if (a.isLive && !b.isLive) return -1;
            if (!a.isLive && b.isLive) return 1;
            return a.name.localeCompare(b.name);
        });

        // Render sorted list
        for (const { name, dataName, isLive, isConnected } of channelStatuses) {
            const channelItem = document.createElement("div");
            channelItem.className = "channel-item";

            if (name === currentChannel.replace("#", "")) {
                channelItem.classList.add("active");
            }

            if (isConnected) {
                channelItem.classList.add("connected");
            }

            channelItem.innerHTML = `
            <span class="channel-name">${name}</span>
            <div class="channel-indicators">
                <span class="connection-status ${
                    isConnected ? "connected" : "disconnected"
                }" 
                      title="${
                          isConnected ? "Connected" : "Disconnected"
                      }"></span>
                <span class="live-status ${isLive ? "live" : "offline"}" 
                      title="${isLive ? "Live" : "Offline"}" 
                      data-channel="${dataName}"></span>
            </div>
            <button class="remove-channel" onclick="removeChannel('${name}')">[X]</button>
        `;

            // Click handler for switching channels
            channelItem.addEventListener("click", (e) => {
                if (
                    !e.target.classList.contains("remove-channel") &&
                    !e.target.classList.contains("live-status") &&
                    !e.target.classList.contains("connection-status")
                ) {
                    switchToChannel(name);
                }
            });

            channelList.appendChild(channelItem);
        }
    } finally {
        isRendering = false;
    }
}

// Helper function to update active channel display
function updateActiveChannel(channel) {
    currentChannel = channel;
    if (activeChannelEl) {
        activeChannelEl.textContent = channel || "No channel selected";
    }
}

// Helper function to clear chat messages
function clearChatMessages() {
    if (chatMessages) {
        chatMessages.innerHTML = "";
    }
    messageElements = [];
}

// Helper function to scroll to bottom
function scrollToBottom() {
    if (autoScrollEnabled && chatMessages) {
        chatMessages.scrollTop = chatMessages.scrollHeight;
    }
}

// Update live status for a specific channel
function updateChannelLiveStatus(channel, isLive) {
    const normalizedChannel = channel.startsWith("#") ? channel : "#" + channel;
    const statusIndicator = document.querySelector(
        `[data-channel="${normalizedChannel}"]`
    );

    if (statusIndicator) {
        statusIndicator.className = `live-status ${
            isLive ? "live" : "offline"
        }`;
        statusIndicator.title = isLive ? "Live" : "Offline";
    }
}

// Sort channel list
function sortChannelList() {
    if (!channelList) return;

    const container = channelList;
    const items = Array.from(container.children);

    // Skip if no items or if it's the empty message
    if (
        items.length === 0 ||
        (items[0] && items[0].className === "empty-channels")
    ) {
        return;
    }

    items.sort((a, b) => {
        const aIsConnected = a.classList.contains("connected");
        const bIsConnected = b.classList.contains("connected");

        // Connected channels first
        if (aIsConnected && !bIsConnected) return -1;
        if (!aIsConnected && bIsConnected) return 1;

        const aIsLive = a
            .querySelector(".live-status")
            ?.classList.contains("live");
        const bIsLive = b
            .querySelector(".live-status")
            ?.classList.contains("live");

        // Live channels second
        if (aIsLive && !bIsLive) return -1;
        if (!aIsLive && bIsLive) return 1;

        // Then sort alphabetically
        const aName = a.querySelector(".channel-name")?.textContent || "";
        const bName = b.querySelector(".channel-name")?.textContent || "";
        return aName.localeCompare(bName);
    });

    // Re-append sorted items
    items.forEach((item) => container.appendChild(item));
}

// Helper function to escape regex special characters
function escapeRegExp(string) {
    return string.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Add a message to the chat with ring buffer functionality
function addMessageToChat(message, shouldScroll = true) {
    if (!chatMessages) return;

    const messageEl = document.createElement("div");
    messageEl.className = "chat-message";

    const usernameColor = message.userColor || "#ffffff";
    let contentHtml = escapeHtml(message.content);

    if (message.isHighlighted) {
        messageEl.classList.add("message-highlighted");
        highlightChannel(message.channel);
    }

    if (message.emotes) {
        for (const [emoteName, base64] of Object.entries(message.emotes)) {
            const escapedName = escapeRegExp(emoteName);
            const regex = new RegExp(`\\b${escapedName}\\b`, "g");
            contentHtml = contentHtml.replace(
                regex,
                `<img src="${base64}" 
                      alt="${emoteName}" 
                      class="emote" 
                      title="${emoteName}"/>`
            );
        }
    }

    messageEl.innerHTML = `
        <span class="timestamp">[${message.timestamp}]</span>
        <span class="username" style="color: ${usernameColor}">${message.username}:</span>
        <span class="message-content">${contentHtml}</span>
    `;

    chatMessages.appendChild(messageEl);
    messageElements.push(messageEl);

    if (messageElements.length > maxMessages) {
        const oldestMessage = messageElements.shift();
        if (oldestMessage?.parentNode) {
            oldestMessage.parentNode.removeChild(oldestMessage);
        }
    }

    // Only scroll if user is at bottom and shouldScroll is true
    if (shouldScroll && autoScrollEnabled) {
        chatMessages.scrollTop = chatMessages.scrollHeight;
    }
}

// Add a reward redemption to chat
function addRewardToChat(reward) {
    if (!chatMessages) return;

    const rewardEl = document.createElement("div");
    rewardEl.className = "reward-message";

    rewardEl.innerHTML = `
        <span class="timestamp">[${reward.timestamp}]</span>
        <strong>🎁 ${reward.username}</strong> redeemed 
        <strong>${reward.rewardName}</strong>
        ${reward.userInput ? `: ${escapeHtml(reward.userInput)}` : ""}
    `;

    chatMessages.appendChild(rewardEl);
    if (autoScrollEnabled) {
        chatMessages.scrollTop = chatMessages.scrollHeight;
    }
}

// Add a system message
function addSystemMessage(message) {
    if (!chatMessages) return;

    const messageEl = document.createElement("div");
    messageEl.className = "chat-message system-message";
    messageEl.innerHTML = `
        <span class="timestamp">[${getCurrentTime()}]</span>
        <span class="system-text">SYSTEM: ${message}</span>
    `;

    chatMessages.appendChild(messageEl);
    if (autoScrollEnabled) {
        chatMessages.scrollTop = chatMessages.scrollHeight;
    }
}

// Update connection status
function updateConnectionStatus(connected, status = null) {
    const viewerCount = document.getElementById("viewer-count");

    if (status === "connecting") {
        if (viewerCount) viewerCount.style.display = "none";
    } else if (connected) {
        if (viewerCount) viewerCount.style.display = "inline";
    } else {
        if (viewerCount) viewerCount.style.display = "none";
    }
}

// Show/hide loading overlay
function showLoading(show) {
    if (loadingOverlay) {
        loadingOverlay.style.display = show ? "flex" : "none";
    }
}

// Show error message
function showError(message) {
    console.error(message);
    addSystemMessage(`ERROR: ${message}`);
}

// Utility functions
function escapeHtml(text) {
    const div = document.createElement("div");
    div.textContent = text;
    return div.innerHTML;
}

function getCurrentTime() {
    const now = new Date();
    return now.toTimeString().slice(0, 8);
}

// Set up scroll listener
if (chatMessages) {
    chatMessages.addEventListener("scroll", () => {
        autoScrollEnabled = isAtBottom();
    });
}

window.removeChannel = removeChannel;
