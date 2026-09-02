class Value {};
class Other {};
class Main {
  main() : Object { case new Value of other : Other => other; esac };
};
