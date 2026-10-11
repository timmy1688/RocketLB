import { createRouter, createWebHistory } from "vue-router";
import ConsoleLayout from "../layouts/ConsoleLayout.vue";
import LoginView from "../views/LoginView.vue";
import OverviewView from "../views/OverviewView.vue";
import NodesView from "../views/NodesView.vue";
import ClustersView from "../views/ClustersView.vue";
import ClusterView from "../views/cluster/ClusterView.vue";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", component: LoginView, meta: { public: true } },
    {
      path: "/",
      component: ConsoleLayout,
      children: [
        { path: "", component: OverviewView },
        { path: "nodes", component: NodesView },
        { path: "clusters", component: ClustersView },
        { path: "clusters/:id", component: ClusterView },
      ],
    },
  ],
});

router.beforeEach((to) => {
  const token = localStorage.getItem("rocketlb.token");
  if (!to.meta.public && !token) return "/login";
  if (to.path === "/login" && token) return "/";
});
